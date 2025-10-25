package netguard

import (
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
	"github.com/iotames/easygo/netguard/log"
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
	go cleanTrafficMap(0)
}

func Run() {
	// 1. 获取网络接口列表并选择（这里选择第一个非环回接口为例）
	dev := GetDefaultDevice()
	log.Info("开始监控：", "设备", dev.Name, "详情", dev.Description)

	// 2. 打开设备进行捕获
	// dev.Name 要监控的网络接口
	// 1600 每个数据包最多捕获 1600 字节（略大于标准 MTU 1500 字节）
	// true 开启混杂模式，捕获所有经过网卡的数据包
	// pcap.BlockForever 超时时间。无限期等待数据包，不设置超时
	handle, err := pcap.OpenLive(dev.Name, 1600, true, pcap.BlockForever)
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
}
