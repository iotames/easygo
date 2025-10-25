package netguard

import (
	"fmt"
	"net"

	"github.com/iotames/easygo/netguard/log"
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
func findPidByConnection(ip net.IP, port uint16) int32 {
	// 这里需要实现从 updateProcessConnectionMap 维护的映射表中查找PID的逻辑
	key := fmt.Sprintf("%s:%d", ip.String(), port)
	if pid, exists := connectionMap.Load(key); exists {
		if pidInt, ok := pid.(int32); ok {
			return pidInt
		}
	}
	// 返回0表示未找到
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
