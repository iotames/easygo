package netguard

import (
	"fmt"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/iotames/easygo/netguard/log"
	"github.com/shirou/gopsutil/v3/process"
)

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
	pid = findPidByConnection(localIP, localPort)
	if pid > 0 {
		proc, err := process.NewProcess(pid)
		if err == nil {
			processName, _ = proc.Name()
		}
	}

	// 更新流量统计
	updatePacketRecord(localIP, localPort, remoteIP, remotePort, protocol.String(), processName, pid, packetLength, isInbound)
}

// updatePacketRecord 更新流量统计信息
func updatePacketRecord(localIP net.IP, localPort uint16, remoteIP net.IP, remotePort uint16, protocol, processName string, pid int32, traffic uint64, isInbound bool) {
	var direction, arrow string
	if isInbound {
		direction = "入站"
		arrow = "<-"
	} else {
		direction = "出站"
		arrow = "->"
	}
	key := fmt.Sprintf("%s:%d", localIP.String(), localPort)

	record, exists := trafficMap.Load(key)
	if !exists {
		// 新建连接
		if realTimeProcessQuery && pid == 0 {
			// 强制查询进程信息
			pid = queryProcessRealTime(localIP, localPort)
			if pid > 0 {
				proc, err := process.NewProcess(pid)
				if err == nil {
					processName, _ = proc.Name()
				}
			}
		}
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
		msg := fmt.Sprintf("新建连接%s：", arrow)
		log.Info(msg, "方向", direction, "本地IP", localIP, "本地端口", localPort, "远程IP", remoteIP, "远程端口", remotePort, "进程", processName, "PID", pid, "字节大小", traffic)
	}

	if tr, ok := record.(*TrafficRecord); ok {
		tr.Lock()
		defer tr.Unlock()

		if isInbound {
			tr.BytesReceived += traffic
		} else {
			tr.BytesSent += traffic
		}

		// 基于时间间隔的日志：每10秒记录一次该连接的流量统计
		if time.Since(tr.LastLogTime) > 10*time.Second {
			// 记录流量统计而非单个包
			msg := fmt.Sprintf("流量统计%s：", arrow)
			log.Debug(msg, "方向", direction, "本地端口", localPort, "远程IP", remoteIP, "远程端口", remotePort, "协议", protocol,
				"进程", processName, "PID", pid, "累计发送字节", tr.BytesSent, "累计接收字节", tr.BytesReceived, "当前包字节", traffic)
			tr.LastLogTime = time.Now()
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
