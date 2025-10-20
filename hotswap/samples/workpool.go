package main

import (
	"fmt"
	"hotswap"
	"time"
)

// 使用示例
func main() {
	// 创建配置
	config := hotswap.Config{
		MinWorkers: 24,  // 根据文章推荐的8核服务器配置
		QueueSize:  240, // workers * 10
	}

	// 创建工作池
	pool := hotswap.New(config)
	pool.Start()
	defer pool.Stop()

	// 提交任务
	task := hotswap.TaskFunc(func() {
		// 处理业务逻辑
		fmt.Println("处理业务逻辑")
		time.Sleep(10 * time.Millisecond)
	})

	// 阻塞提交
	pool.Submit(task)

	// 非阻塞提交
	if !pool.TrySubmit(task) {
		// 处理队列满的情况
	}

	// 带超时提交
	if !pool.SubmitWithTimeout(task, 100*time.Millisecond) {
		// 处理超时
	}
}
