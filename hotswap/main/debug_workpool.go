package main

import (
	"fmt"
	"hotswap"
	"time"
)

// 对于大多数 Web 服务：
// CPU 密集型任务：CPU 核心数 × 1-2。
// I/O 密集型任务：CPU 核心数 × 2-4。
// 混合型工作负载：从 CPU 核心数 × 2 开始，然后进行基准测试。
//
// 测试发现 8 核生产服务器在处理 I/O 密集的 API 时，使用 24 个工作线程表现最佳。这个看似很小的数字，却能处理超过每秒 100 万请求，同时保持 P99 延迟在 10 毫秒以下。
// 使用示例
func debug_workerpool() {

	// TODO 可集成服务器性能监控服务。
	// 在实际应用中，可以通过监控以下指标来动态调整这些参数：
	// 1. 内存使用情况
	// 2. 队列大小变化 (QueueSize() 方法可以获取)
	// 3. TrySubmit 或 SubmitWithTimeout 的失败率
	// 4. 任务处理延迟
	// 如内存使用率飙升，适当降低QueueSize或MinWorkers。
	// 内存占用较低，但队列经常爆满，则适当增加QueueSize或MinWorkers

	// 创建配置
	// 8核服务器：workers = 24, QueueSize = workers * 10 = 240
	config := hotswap.Config{
		MinWorkers: 24,  // 根据文章推荐的8核服务器配置
		QueueSize:  240, // workers * 10
	}

	// 创建工作池
	pool := hotswap.NewWorkerPool(config)
	pool.Start()
	defer pool.Stop()

	// 提交任务
	task := hotswap.TaskFunc(func() {
		// 处理业务逻辑
		fmt.Println(time.Now().Format("SUCCESS: 处理业务逻辑 On: " + time.DateTime))
		time.Sleep(10 * time.Millisecond)
	})

	// 非阻塞提交
	for i := 0; i < 100; i++ {
		go func(ii int) {
			// 非阻塞提交
			if !pool.TrySubmit(task) {
				// 处理队列满的情况
				fmt.Println(ii, time.Now().Format("ERROR: 队列已满 On: "+time.DateTime))
			}
		}(i)
	}

	// // 阻塞提交
	// pool.Submit(task)

	// // 带超时提交
	// // 0.2秒超时
	// if !pool.SubmitWithTimeout(task, 200*time.Millisecond) {
	// 	// 处理超时
	// }
}
