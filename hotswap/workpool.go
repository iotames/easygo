package hotswap

import (
	"context"
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
	close(wp.quit)
	wp.wg.Wait()
}

func (wp *WorkerPool) worker() {
	defer wp.wg.Done()

	for {
		select {
		case task := <-wp.taskQueue:
			// 原子操作，更新队列大小
			atomic.StoreInt32(&wp.queueSize, int32(len(wp.taskQueue)))
			task.Execute()
		case <-wp.quit:
			return
		}
	}
}
