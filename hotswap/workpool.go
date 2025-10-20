package hotswap

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// https://mp.weixin.qq.com/s/7WKOxgOzU329a-PoaVmojQ
// 核心观点​​：通过限制 goroutine 数量来提升性能，而非无限创建 goroutine
// 内存减少 85%，吞吐量提升 40 倍
// 最佳 worker 数 = (CPU 核心数 × 2) + 阻塞 I/O 操作数

// Task 任务接口
type Task interface {
	Execute()
}

// TaskFunc 函数类型任务适配器
type TaskFunc func()

func (f TaskFunc) Execute() { f() }

// WorkerPool 工作池
type WorkerPool struct {
	workers    int
	taskQueue  chan Task
	quit       chan struct{}
	wg         sync.WaitGroup
	queueSize  int32
	maxWorkers int
	mutex      sync.RWMutex
	stateMutex sync.Mutex // 保护状态变更操作
	stopped    bool       // 标记是否已经停止
}

// Config 配置
type Config struct {
	MinWorkers int // 最小工作线程数
	MaxWorkers int // 最大工作线程数（动态扩展用）
	QueueSize  int // 队列大小
}

// NewWorkerPool 创建工作池
func NewWorkerPool(config Config) *WorkerPool {
	if config.MinWorkers <= 0 {
		config.MinWorkers = runtime.NumCPU() * 2
	}
	if config.QueueSize <= 0 {
		config.QueueSize = config.MinWorkers * 10
	}

	return &WorkerPool{
		workers:    config.MinWorkers,
		maxWorkers: config.MaxWorkers,
		taskQueue:  make(chan Task, config.QueueSize),
		quit:       make(chan struct{}),
	}
}

// Start 启动工作池
func (wp *WorkerPool) Start() {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
}

// Submit 提交任务（阻塞）
// 如果任务队列已满，则会等待直至任务队列有空闲位置。
func (wp *WorkerPool) Submit(task Task) {
	wp.taskQueue <- task
	// 原子操作，更新队列大小
	atomic.StoreInt32(&wp.queueSize, int32(len(wp.taskQueue)))
}

// TrySubmit 尝试提交任务（非阻塞）
// 如果任务队列已满，则不等待，立即返回false。
// 也可使用SubmitWithTimeout。然后把超时失败的任务放到监控统计中，以便后续优化。
func (wp *WorkerPool) TrySubmit(task Task) bool {
	select {
	case wp.taskQueue <- task:
		// 原子操作，更新队列大小
		atomic.StoreInt32(&wp.queueSize, int32(len(wp.taskQueue)))
		return true
	default:
		return false
	}
}

// SubmitWithTimeout 带超时提交
// 如果任务队列已满，则指定等待时间（默认3秒），如超过等待时间，且队列仍然满则放弃。返回false。
// 例如等待时间超3秒，已严重影响用户体验。放弃后加入监控统计的失败池中，以便后续优化。
func (wp *WorkerPool) SubmitWithTimeout(task Task, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case wp.taskQueue <- task:
		// 原子操作，更新队列大小
		atomic.StoreInt32(&wp.queueSize, int32(len(wp.taskQueue)))
		return true
	case <-ctx.Done():
		return false
	}
}

// QueueSize 获取当前队列大小
func (wp *WorkerPool) QueueSize() int {
	// 原子操作，获取队列大小
	return int(atomic.LoadInt32(&wp.queueSize))
}

// Stop 停止工作池
func (wp *WorkerPool) Stop() {
	wp.stateMutex.Lock()
	wp.stopped = true // 添加这行
	wp.stateMutex.Unlock()
	close(wp.quit)
	wp.wg.Wait()

	// 处理队列中剩余的任务
	close(wp.taskQueue)
	for task := range wp.taskQueue {
		task.Execute()
	}
}

func (wp *WorkerPool) worker() {
	defer wp.wg.Done()

	for {
		select {
		case task := <-wp.taskQueue:
			// 原子操作，更新队列大小
			// TODO 读取 len(wp.taskQueue) 和实际的队列操作不是原子的，可能导致队列大小不准确。此问题应该无伤大雅。
			atomic.StoreInt32(&wp.queueSize, int32(len(wp.taskQueue)))
			task.Execute()
		case <-wp.quit:
			return
		}
	}
}

// UpdateWorkers 动态更新工作协程数量
func (wp *WorkerPool) UpdateWorkers(num int) error {
	if num <= 0 {
		num = runtime.NumCPU() * 2
	}

	// 限制不超过最大工作协程数
	if wp.maxWorkers > 0 && num > wp.maxWorkers {
		num = wp.maxWorkers
	}

	wp.stateMutex.Lock()
	defer wp.stateMutex.Unlock()

	if wp.stopped {
		return fmt.Errorf("worker pool is stopped")
	}

	oldWorkers := wp.workers

	// 如果增加worker，直接启动新的worker
	if num > oldWorkers {
		wp.workers = num
		for i := oldWorkers; i < num; i++ {
			wp.wg.Add(1)
			go wp.worker()
		}
		return nil
	}

	// 如果减少worker，需要重建工作池
	if num < oldWorkers {
		return wp.rebuildWorkerPool(num, cap(wp.taskQueue))
	}

	return nil
}

// UpdateQueueSize 动态更新队列大小
func (wp *WorkerPool) UpdateQueueSize(size int) error {
	if size <= 0 {
		return fmt.Errorf("queue size must be positive")
	}

	wp.stateMutex.Lock()
	defer wp.stateMutex.Unlock()

	if wp.stopped {
		return fmt.Errorf("worker pool is stopped")
	}

	currentCap := cap(wp.taskQueue)

	// 如果大小相同，无需更改
	if currentCap == size {
		return nil
	}

	// 重建工作池，保持当前的worker数量
	return wp.rebuildWorkerPool(wp.workers, size)
}

// rebuildWorkerPool 重建工作池，这是关键的实现部分
func (wp *WorkerPool) rebuildWorkerPool(workerCount, queueSize int) error {
	// 1. 创建临时队列，收集新任务（缓冲更大防止阻塞）
	tempQueue := make(chan Task, queueSize*2)

	// 2. 用 stateMutex 保护状态变更
	wp.stateMutex.Lock()
	wp.stopped = true
	wp.stateMutex.Unlock()

	// 3. 用 mutex 加锁，保护队列切换的状态变更。切断原队列的新任务添加
	wp.mutex.Lock()
	// 创建一个新的变量 oldTaskQueue，它指向原来的任务队列。
	// 这样，我们可以通过 oldTaskQueue 继续处理原队列中剩余的任务
	oldTaskQueue := wp.taskQueue
	wp.taskQueue = tempQueue
	// 信道为引用类型。将 wp.taskQueue 指向新的临时队列 tempQueue。
	// 这样，在重建过程中，新提交的任务就会进入 tempQueue，而不会进入旧队列。
	wp.mutex.Unlock()

	// 4. 排空原队列
	pendingTasks := wp.drainTaskQueue(oldTaskQueue)

	// 5. 发送停止信号给所有worker，停止原worker池
	close(wp.quit)
	// 等待所有原worker结束
	wp.wg.Wait()

	// 6. 重建工作池（不需要加锁，因为stateMutex已保证单线程）
	wp.taskQueue = make(chan Task, queueSize)
	wp.quit = make(chan struct{})
	wp.workers = workerCount

	// 7. 合并任务：原队列剩余任务 + 临时队列新任务
	wp.mergeTasks(pendingTasks, tempQueue)

	// 8. 启动新worker
	for i := 0; i < workerCount; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}

	// 9. 重置状态
	wp.stateMutex.Lock()
	wp.stopped = false
	wp.stateMutex.Unlock()

	// 更新队列大小
	atomic.StoreInt32(&wp.queueSize, int32(len(wp.taskQueue)))

	return nil
}

// drainTaskQueue 排空原队列的剩余任务
func (wp *WorkerPool) drainTaskQueue(queue chan Task) []Task {
	var tasks []Task
	close(queue) // 关闭以便遍历
	for task := range queue {
		tasks = append(tasks, task)
	}
	return tasks
}

func (wp *WorkerPool) mergeTasks(pendingTasks []Task, tempQueue chan Task) {
	// 先处理原队列剩余任务
	for _, task := range pendingTasks {
		select {
		case wp.taskQueue <- task:
		default:
			// TODO 新队列已满，任务丢弃。记录到日志和数据库
			log.Printf("Task dropped during pool rebuild")
		}
	}

	// 再处理临时队列中的新任务
	close(tempQueue)
	for task := range tempQueue {
		select {
		case wp.taskQueue <- task:
		default:
			// TODO 新队列已满，任务丢弃。记录到日志和数据库
			log.Printf("Task dropped during pool rebuild")
		}
	}
}
