package netguard

import (
	"fmt"
	"net"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// 添加测试：解析 IPv4+TCP 数据包，验证 getPacketNetworkInfo 返回正确的源/目的 IP 和协议
func TestGetPacketNetworkInfoIPv4(t *testing.T) {
	ip := &layers.IPv4{
		SrcIP:    net.IPv4(10, 0, 0, 1),
		DstIP:    net.IPv4(10, 0, 0, 2),
		Protocol: layers.IPProtocolTCP,
		TTL:      64,
		Version:  4,
	}
	tcp := &layers.TCP{
		SrcPort: layers.TCPPort(12345),
		DstPort: layers.TCPPort(80),
		Seq:     1,
	}
	// 为校验和设置网络层
	tcp.SetNetworkLayerForChecksum(ip)

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if err := gopacket.SerializeLayers(buf, opts, ip, tcp, gopacket.Payload([]byte("hello"))); err != nil {
		t.Fatalf("序列化数据包失败: %v", err)
	}
	packet := gopacket.NewPacket(buf.Bytes(), layers.LayerTypeIPv4, gopacket.Default)

	src, dst, proto, ok := getPacketNetworkInfo(packet)
	if !ok {
		t.Fatal("getPacketNetworkInfo 未能识别 IPv4 网络层")
	}
	if !src.Equal(ip.SrcIP) {
		t.Fatalf("源 IP 不匹配，期望 %v，实际 %v", ip.SrcIP, src)
	}
	if !dst.Equal(ip.DstIP) {
		t.Fatalf("目的 IP 不匹配，期望 %v，实际 %v", ip.DstIP, dst)
	}
	if proto != layers.IPProtocolTCP {
		t.Fatalf("协议不匹配，期望 %v，实际 %v", layers.IPProtocolTCP, proto)
	}
}

// 添加测试：设置 localIPs 并校验 isLocalIP 行为
func TestIsLocalIP(t *testing.T) {
	// 设置 localIPs（需加锁）
	localIPsMutex.Lock()
	localIPs = []net.IP{net.IPv4(192, 168, 0, 2)}
	localIPsMutex.Unlock()

	if !isLocalIP(net.IPv4(192, 168, 0, 2)) {
		t.Fatal("isLocalIP 对本地 IP 应返回 true")
	}
	if isLocalIP(net.IPv4(8, 8, 8, 8)) {
		t.Fatal("isLocalIP 对非本地 IP 应返回 false")
	}
}

// 添加测试：调用 updatePacketRecord 并验证 trafficMap 中的记录
func TestUpdatePacketRecordCreatesRecord(t *testing.T) {
	localIP := net.IPv4(10, 0, 0, 5)
	remoteIP := net.IPv4(8, 8, 8, 8)
	localPort := uint16(54321)
	remotePort := uint16(53)
	protocol := "udp"
	processName := "testproc"
	pid := int32(12345)
	traffic := uint64(500)

	key := fmt.Sprintf("%s:%d", localIP.String(), localPort)
	// 先清理可能存在的旧记录
	trafficMap.Delete(key)

	updatePacketRecord(localIP, localPort, remoteIP, remotePort, protocol, processName, pid, traffic, false)

	v, ok := trafficMap.Load(key)
	if !ok {
		t.Fatalf("更新后未在 trafficMap 中找到 key=%s 的记录", key)
	}
	tr, ok := v.(*TrafficRecord)
	if !ok {
		t.Fatalf("trafficMap 中的值无法转换为 *TrafficRecord")
	}
	// 检查字段
	if tr.ProcessName != processName {
		t.Fatalf("ProcessName 不匹配，期望 %s，实际 %s", processName, tr.ProcessName)
	}
	if tr.ProcessPID != pid {
		t.Fatalf("ProcessPID 不匹配，期望 %d，实际 %d", pid, tr.ProcessPID)
	}
	if tr.BytesSent != traffic {
		t.Fatalf("BytesSent 不匹配，期望 %d，实际 %d", traffic, tr.BytesSent)
	}
}
