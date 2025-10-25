package log

import (
	"log/slog"
	"os"
	"sync"
	"time"
)

var (
	lg   *slog.Logger
	once sync.Once
)

// Debug 记录调试级别日志
func Debug(msg string, args ...any) {
	getLogger().Debug(msg, args...)
}

// Info 记录信息级别日志
func Info(msg string, args ...any) {
	getLogger().Info(msg, args...)
}

// Warn 记录警告级别日志
func Warn(msg string, args ...any) {
	getLogger().Warn(msg, args...)
}

// Error 记录错误级别日志
func Error(msg string, args ...any) {
	getLogger().Error(msg, args...)
}

// getLogger 获取单例日志实例
func getLogger() *slog.Logger {
	once.Do(func() {
		if lg == nil {

			// 设置 HandlerOptions，自定义时间属性
			opts := &slog.HandlerOptions{
				Level: slog.LevelDebug,

				ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
					// 如果当前属性是时间戳
					if a.Key == slog.TimeKey && len(groups) == 0 {
						a.Key = "time" // 键名可以保持不变或修改
						// 将时间值转换为自定义格式
						if t, ok := a.Value.Any().(time.Time); ok {
							a.Value = slog.StringValue(t.Format(time.DateTime))
						}
					}
					return a
				},
			}

			lg = slog.New(slog.NewTextHandler(os.Stdout, opts))

		}
	})
	return lg
}
