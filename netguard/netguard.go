package netguard

import (
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/iotames/easygo/netguard/log"
	gnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// TrafficRecord 记录流量信息
type TrafficRecord struct {
	sync.RWMutex
	LocalIP       net.IP
	LocalPort     uint16
	RemoteIP      net.IP
	RemotePort    uint16
	Protocol      string
	ProcessName   string
	ProcessPID    int32
	BytesSent     uint64
	BytesReceived uint64
	LastUpdate    time.Time
	LastLogTime   time.Time
}

// 全局变量
var (
	trafficMap    sync.Map // key: "LocalIP:LocalPort" string, value: *TrafficRecord
	connectionMap sync.Map // key: "IP:Port" string, value: int32 (PID)
	localIPs      []net.IP // 缓存本地IP列表
)

func init() {
	// 初始化时获取本地IP
	updateLocalIPs()
	// 定期清理长时间未更新的trafficMap记录
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			trafficMap.Range(func(key, value interface{}) bool {
				if record, ok := value.(*TrafficRecord); ok {
					record.RLock()
					if time.Since(record.LastUpdate) > 5*time.Minute {
						trafficMap.Delete(key)
					}
					record.RUnlock()
				}
				return true
			})
		}
	}()
}

func Run() {
	// 1. 获取网络接口列表并选择（这里选择第一个非环回接口为例）
	devices, err := pcap.FindAllDevs()
	// for _, device := range devices {
	// 	log.Debug("设备信息", "名称", device.Name, "详情", device.Description, "地址", device.Addresses)
	// }
	if err != nil {
		log.Error("无法获取网络设备列表:", "错误", err)
	}
	var deviceName string
	var deviceDesc string
	for _, device := range devices {
		// 跳过虚拟和蓝牙设备
		descLower := strings.ToLower(device.Description)
		if strings.Contains(descLower, "bluetooth") ||
			strings.Contains(descLower, "virtual") ||
			strings.Contains(descLower, "loopback") {
			continue
		}
		// 选择第一个有IP地址的非环回设备
		if len(device.Addresses) > 0 {
			for _, addr := range device.Addresses {
				if addr.IP != nil && !addr.IP.IsLoopback() {
					deviceName = device.Name
					deviceDesc = device.Description
					break
				}
			}
		}
		if deviceName != "" {
			break
		}
	}
	if deviceName == "" {
		log.Error("未找到合适的网络设备")
	}
	log.Info("开始监控：", "设备", deviceName, "详情", deviceDesc)

	// 2. 打开设备进行捕获
	// 参数：设备名、快照长度（字节）、是否开启混杂模式、超时时间
	handle, err := pcap.OpenLive(deviceName, 1600, true, pcap.BlockForever)
	if err != nil {
		log.Error("打开设备失败:", "错误", err)
	}
	defer handle.Close()

	// 可设置BPF过滤器，例如 "tcp or udp"
	err = handle.SetBPFFilter("tcp or udp")
	if err != nil {
		log.Warn("设置过滤器失败（继续执行）: ", "错误", err)
	}

	// 3. 定期更新进程连接映射表（因为进程连接会动态变化）
	go updateProcessConnectionMap()
	// 定期更新本地IP
	go periodicallyUpdateLocalIPs()

	// 4. 创建数据包源并开始处理
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	numCPU := runtime.NumCPU()
	workerPoolNum := numCPU * 2
	log.Info("开始处理数据包：", "CPU核心数", numCPU, "工作池数", workerPoolNum)

	// 创建worker池
	packetChan := make(chan gopacket.Packet, 1000) // 缓冲队列
	for i := 0; i < workerPoolNum; i++ {
		go func() {
			for packet := range packetChan {
				processCapturedPacket(packet)
			}
		}()
	}
	// 在packetSource循环中发送到channel
	for packet := range packetSource.Packets() {
		packetChan <- packet
	}
	// for packet := range packetSource.Packets() {
	// 	// 并发处理每个包，生产环境考虑用goroutine池
	// 	go processCapturedPacket(packet)
	// }
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

// periodicallyUpdateLocalIPs 定期更新本地IP列表
func periodicallyUpdateLocalIPs() {
	ticker := time.NewTicker(30 * time.Second)
	for {
		<-ticker.C
		updateLocalIPs()
	}
}

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

// processCapturedPacket 处理捕获到的数据包
func processCapturedPacket(packet gopacket.Packet) {
	// 添加recover防止单个包处理失败影响整个程序
	defer func() {
		if r := recover(); r != nil {
			log.Warn("处理数据包时发生panic:", "panic", r)
		}
	}()

	// 获取网络层（IP）信息
	ipLayer := packet.NetworkLayer()
	if ipLayer == nil {
		return
	}

	var srcIP, dstIP net.IP
	var protocol layers.IPProtocol

	switch v := ipLayer.(type) {
	case *layers.IPv4:
		srcIP = v.SrcIP
		dstIP = v.DstIP
		protocol = v.Protocol
	case *layers.IPv6:
		srcIP = v.SrcIP
		dstIP = v.DstIP
		protocol = v.NextHeader
	default:
		return
	}

	// 获取传输层（TCP/UDP）信息
	var srcPort, dstPort uint16
	switch transportLayer := packet.TransportLayer().(type) {
	case *layers.TCP:
		srcPort = uint16(transportLayer.SrcPort)
		dstPort = uint16(transportLayer.DstPort)
	case *layers.UDP:
		srcPort = uint16(transportLayer.SrcPort)
		dstPort = uint16(transportLayer.DstPort)
	default:
		return
	}

	packetLength := uint64(len(packet.Data()))

	// 判断流量方向（简化逻辑：假设目的IP是本机则为入流量）
	isInbound := isLocalIP(dstIP) // 需要实现 isLocalIP 函数检查 dstIP 是否为本地IP

	// 确定本地和远程地址
	var localIP, remoteIP net.IP
	var localPort, remotePort uint16

	if isInbound {
		// 入流量，通过目的IP和端口查找进程
		localIP, localPort, remoteIP, remotePort = dstIP, dstPort, srcIP, srcPort
	} else {
		// 出流量，通过源IP和端口查找进程
		localIP, localPort, remoteIP, remotePort = srcIP, srcPort, dstIP, dstPort
	}
	// 关键：通过连接映射表查找进程信息
	var pid int32
	var processName string
	pid = findPIDByConnection(localIP, localPort)
	if pid > 0 {
		proc, err := process.NewProcess(pid)
		if err == nil {
			processName, _ = proc.Name()
		}
	}

	// 更新流量统计
	updateTrafficStats(localIP, localPort, remoteIP, remotePort, protocol.String(), processName, pid, packetLength, isInbound)
}

// findPIDByConnection 通过IP和端口查找对应的进程PID
func findPIDByConnection(ip net.IP, port uint16) int32 {
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

// updateTrafficStats 更新流量统计信息
func updateTrafficStats(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, protocol, processName string, pid int32, traffic uint64, isInbound bool) {
	key := fmt.Sprintf("%s:%d", localIP.String(), localPort)

	record, exists := trafficMap.Load(key)
	if !exists {
		record = &TrafficRecord{
			LocalIP:     localIP,
			LocalPort:   localPort,
			RemoteIP:    remoteIP,
			RemotePort:  remotePort,
			Protocol:    protocol,
			ProcessName: processName,
			ProcessPID:  pid,
			LastUpdate:  time.Now(),
		}
		trafficMap.Store(key, record)

		// 新连接日志
		direction := "出站"
		arrow := "->"
		if isInbound {
			direction = "入站"
			arrow = "<-"
		}
		msg := fmt.Sprintf("新建连接%s：", arrow)
		log.Info(msg, "方向", direction, "本地IP", localIP, "本地端口", localPort, "远程IP", remoteIP, "远程端口", remotePort, "进程", processName, "PID", pid, "字节大小", traffic)
	}

	if tr, ok := record.(*TrafficRecord); ok {
		tr.Lock()
		defer tr.Unlock()

		// 基于时间间隔的日志：每3秒记录一次该连接的流量摘要
		if time.Since(tr.LastLogTime) > 3*time.Second {
			direction := "出站"
			arrow := "->"
			if isInbound {
				direction = "入站"
				arrow = "<-"
			}

			// 记录流量摘要而非单个包
			msg := fmt.Sprintf("流量摘要%s：", arrow)
			log.Debug(msg, "方向", direction, "本地端口", localPort, "远程IP", remoteIP, "远程端口", remotePort, "协议", protocol,
				"进程", processName, "PID", pid, "累计发送字节", tr.BytesSent, "累计接收字节", tr.BytesReceived, "当前包字节", traffic)
			tr.LastLogTime = time.Now()
		}
		if isInbound {
			tr.BytesReceived += traffic
		} else {
			tr.BytesSent += traffic
		}
		tr.LastUpdate = time.Now()
		// 更新其他可能变化的信息
		if processName != "" {
			tr.ProcessName = processName
		}
		if pid > 0 {
			tr.ProcessPID = pid
		}
	}
}

// GetTrafficStats 获取流量统计信息（用于外部访问）
func GetTrafficStats() []*TrafficRecord {
	var stats []*TrafficRecord
	trafficMap.Range(func(key, value interface{}) bool {
		if record, ok := value.(*TrafficRecord); ok {
			// 创建副本避免并发问题
			record.RLock()
			stat := &TrafficRecord{
				LocalIP:       record.LocalIP,
				LocalPort:     record.LocalPort,
				RemoteIP:      record.RemoteIP,
				RemotePort:    record.RemotePort,
				Protocol:      record.Protocol,
				ProcessName:   record.ProcessName,
				ProcessPID:    record.ProcessPID,
				BytesSent:     record.BytesSent,
				BytesReceived: record.BytesReceived,
				LastUpdate:    record.LastUpdate,
			}
			record.RUnlock()
			stats = append(stats, stat)
		}
		return true
	})
	return stats
}
