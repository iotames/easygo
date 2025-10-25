package netguard

import (
	"fmt"
	"net"
	"time"

	"github.com/iotames/easygo/netguard/log"
	gnet "github.com/shirou/gopsutil/v3/net"
)

// cleanTrafficMap 定期清理长时间未更新的trafficMap记录
func cleanTrafficMap(d time.Duration) {
	if d <= 0 {
		d = 10 * time.Minute
	}
	ticker := time.NewTicker(d)
	for range ticker.C {
		trafficMap.Range(func(key, value interface{}) bool {
			if record, ok := value.(*TrafficRecord); ok {
				record.RLock()
				if time.Since(record.LastUpdate) > d {
					trafficMap.Delete(key)
				}
				record.RUnlock()
			}
			return true
		})
	}
}

// periodicallyUpdateLocalIPs 定期更新本地IP列表
func periodicallyUpdateLocalIPs() {
	ticker := time.NewTicker(30 * time.Second)
	for {
		<-ticker.C
		updateLocalIPs()
	}
}

// updateProcessConnectionMap 定期更新网络连接与进程的映射关系
func updateProcessConnectionMap() {
	ticker := time.NewTicker(5 * time.Second)
	for {
		<-ticker.C
		connections, err := gnet.Connections("all")
		if err != nil {
			log.Warn("获取网络连接信息失败:", "错误", err)
			continue
		}

		// 临时map用于批量更新
		tempMap := make(map[string]int32)

		for _, conn := range connections {
			if conn.Laddr.IP != "" && conn.Laddr.Port != 0 {
				// 标准化IP格式
				ip := net.ParseIP(conn.Laddr.IP)
				if ip != nil {
					key := fmt.Sprintf("%s:%d", ip.String(), conn.Laddr.Port)
					tempMap[key] = conn.Pid
				}
			}
		}

		// 原子性更新全局映射
		for key, pid := range tempMap {
			connectionMap.Store(key, pid)
		}

		// 清理过期的连接（可选）
		connectionMap.Range(func(key, value interface{}) bool {
			if _, exists := tempMap[key.(string)]; !exists {
				connectionMap.Delete(key)
			}
			return true
		})
	}
}
