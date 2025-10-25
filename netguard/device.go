package netguard

import (
	"strings"

	"github.com/google/gopacket/pcap"
	"github.com/iotames/easygo/netguard/log"
)

// GetDefaultDevice 获取默认的网络设备。选择第一个非环回接口。
func GetDefaultDevice() pcap.Interface {
	devices, err := pcap.FindAllDevs()
	// for _, device := range devices {
	// 	log.Debug("设备信息", "名称", device.Name, "详情", device.Description, "地址", device.Addresses)
	// }
	if err != nil {
		log.Error("无法获取网络设备列表:", "错误", err)
	}

	var defaultDevice pcap.Interface
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
					defaultDevice = device
					break
				}
			}
		}
		if defaultDevice.Name != "" {
			break
		}
	}
	if defaultDevice.Name == "" {
		log.Error("未找到合适的网络设备")
	}
	return defaultDevice
}
