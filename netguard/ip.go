package netguard

import (
	"fmt"
	"net"
	"sync"

	"github.com/iotames/easygo/netguard/log"
	gnet "github.com/shirou/gopsutil/v3/net"
)

// 全局配置
var (
	realTimeProcessQuery = true                   // 实时进程查询开关
	processQueryCache    = make(map[string]int32) // 进程查询缓存
	processCacheMutex    sync.RWMutex
)

// updateLocalIPs 获取本机所有IP地址
func updateLocalIPs() {
	var ips []net.IP

	// 获取所有网络接口
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Error("获取网络接口失败:", "错误", err)
		return
	}

	for _, iface := range ifaces {
		// 跳过环回和未启用的接口
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip != nil && !ip.IsLoopback() {
				ips = append(ips, ip)
			}
		}
	}

	localIPs = ips
}

// findPidByConnection 通过IP和端口查找对应的进程PID
//
// 数据包到达
//
//	↓
//
// processCapturedPacket调用findPIDByConnection
//
//	↓
//
// findPIDByConnection：只查缓存和映射表，返回PID（可能为0）
//
//	↓
//
// 调用updateTrafficStats
//
//	↓
//
// 如果是新建连接 && 开启实时查询 && PID=0 → 触发实时查询
//
//	↓
//
// 更新流量统计
func findPidByConnection(ip net.IP, port uint16) int32 {
	key := fmt.Sprintf("%s:%d", ip.String(), port)

	// 1. 先查缓存（快速路径）
	processCacheMutex.RLock()
	if pid, exists := processQueryCache[key]; exists {
		processCacheMutex.RUnlock()
		return pid
	}
	processCacheMutex.RUnlock()

	// 2. 查全局连接映射表
	if pid, exists := connectionMap.Load(key); exists {
		if pidInt, ok := pid.(int32); ok && pidInt > 0 {
			// 更新缓存
			processCacheMutex.Lock()
			processQueryCache[key] = pidInt
			processCacheMutex.Unlock()
			return pidInt
		}
	}

	// // 3. 如果开启实时查询且缓存未命中，进行实时查询
	// if realTimeProcessQuery {
	// 	return queryProcessRealTime(ip, port)
	// }

	return 0
}

// queryProcessRealTime 实时查询进程信息
func queryProcessRealTime(ip net.IP, port uint16) int32 {
	// 立即查询当前系统连接表
	connections, err := gnet.Connections("all")
	if err != nil {
		log.Warn("获取网络连接信息失败:", "错误", err)
		return 0
	}

	targetIP := ip.String()
	for _, conn := range connections {
		if conn.Laddr.IP == targetIP && conn.Laddr.Port == uint32(port) {
			// 找到匹配连接，更新缓存和全局映射
			key := fmt.Sprintf("%s:%d", ip.String(), port)

			processCacheMutex.Lock()
			processQueryCache[key] = conn.Pid
			processCacheMutex.Unlock()

			connectionMap.Store(key, conn.Pid)
			log.Debug("实时查询PID成功", "key", key, "PID", conn.Pid)
			return conn.Pid
		}
	}

	return 0
}

// isLocalIP 判断一个IP地址是否为本地IP
func isLocalIP(ip net.IP) bool {
	// 实现逻辑：获取本机所有IP地址，检查参数IP是否在其中
	for _, localIP := range localIPs {
		if localIP.Equal(ip) {
			return true
		}
	}
	return false
}
