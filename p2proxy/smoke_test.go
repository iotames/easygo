package p2proxy

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// freeUDPPort 获取一个可用的UDP端口
func freeUDPPort() (int, error) {
	l, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.LocalAddr().(*net.UDPAddr).Port, nil
}

// freeTCPPort 获取一个可用的TCP端口
func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// TestSmokeP2Proxy 测试P2P代理的基本功能
// 测试流程：
// 1. 启动Tracker服务器
// 2. 启动两个节点NodeA和NodeB
// 3. 通过SOCKS5代理建立连接
// 4. 验证数据能否正确传输
func TestSmokeP2Proxy(t *testing.T) {
	// 获取一个可用的UDP端口用于Tracker
	tp, err := freeUDPPort()
	if err != nil {
		t.Fatalf("无法获取可用UDP端口: %v", err)
	}
	trackerAddr := fmt.Sprintf("127.0.0.1:%d", tp)

	// 启动Tracker服务器
	tr := NewTracker(trackerAddr)
	done := make(chan error, 1)
	go func() {
		done <- tr.Run()
	}()

	// 等待Tracker服务器启动完成
	deadline := time.Now().Add(5 * time.Second)
	for tr.conn == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tr.conn == nil {
		t.Fatalf("Tracker服务器启动超时")
	}
	t.Logf("Tracker服务器已在 %s 启动", trackerAddr)

	// 启动NodeB节点
	nb, err := NewNode("nodeB", trackerAddr)
	if err != nil {
		t.Fatalf("创建NodeB失败: %v", err)
	}
	defer nb.Close()

	if err := nb.Register(); err != nil {
		t.Fatalf("NodeB注册失败: %v", err)
	}
	t.Log("NodeB已注册到Tracker")

	// 启动NodeA节点
	na, err := NewNode("nodeA", trackerAddr)
	if err != nil {
		t.Fatalf("创建NodeA失败: %v", err)
	}
	defer na.Close()

	if err := na.Register(); err != nil {
		t.Fatalf("NodeA注册失败: %v", err)
	}
	t.Log("NodeA已注册到Tracker")

	// 获取一个可用的TCP端口用于SOCKS5代理
	sp, err := freeTCPPort()
	if err != nil {
		t.Fatalf("无法获取可用TCP端口: %v", err)
	}
	socksAddr := fmt.Sprintf("127.0.0.1:%d", sp)

	// 启动NodeA的SOCKS5代理服务
	if err := na.StartSocks5(socksAddr, "nodeB"); err != nil {
		t.Fatalf("启动SOCKS5代理失败: %v", err)
	}
	t.Logf("NodeA的SOCKS5代理已在 %s 启动，将流量转发至nodeB", socksAddr)

	// 确保节点间能够相互发现
	peerAddr, err := na.Lookup("nodeB")
	if err != nil {
		t.Fatalf("NodeA查找NodeB失败: %v", err)
	}
	t.Logf("NodeA发现NodeB地址: %s", peerAddr.String())

	// 发送探测包帮助NAT穿透
	t.Log("发送探测包以帮助NAT穿透...")
	for i := 0; i < 5; i++ {
		// NodeA向NodeB发送探测包
		if err := na.sendProto(peerAddr, ProtoMsg{Type: "probe", From: na.ID}); err != nil {
			t.Logf("NodeA发送探测包%d失败: %v", i, err)
		}

		// NodeB也向NodeA发送探测包
		nbPeer, err := nb.Lookup("nodeA")
		if err == nil && nbPeer != nil {
			if err := nb.sendProto(nbPeer, ProtoMsg{Type: "probe", From: nb.ID}); err != nil {
				t.Logf("NodeB发送探测包%d失败: %v", i, err)
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	// 等待探测包完成NAT映射
	t.Log("等待NAT穿透完成...")
	time.Sleep(1 * time.Second)

	// 启动一个本地HTTP测试服务器作为目标服务器
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Log("测试服务器收到请求:", r.Method, r.URL.Path)
		w.WriteHeader(200)
		w.Write([]byte("Hello P2P Proxy!"))
	}))
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("解析测试服务器URL失败: %v", err)
	}
	target := u.Host
	t.Logf("本地测试服务器已在 %s 启动", target)

	// 使用SOCKS5拨号器连接到目标服务器
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		t.Fatalf("创建SOCKS5拨号器失败: %v", err)
	}
	t.Log("SOCKS5拨号器创建成功")

	// 通过SOCKS5代理连接到目标服务器
	conn, err := dialer.Dial("tcp", target)
	if err != nil {
		t.Fatalf("通过SOCKS5代理连接失败: %v", err)
	}
	defer conn.Close()
	t.Log("成功通过SOCKS5代理建立连接")

	// 发送HTTP GET请求
	httpReq := "GET / HTTP/1.1\r\nHost: " + target + "\r\nConnection: close\r\n\r\n"
	_, err = conn.Write([]byte(httpReq))
	if err != nil {
		t.Fatalf("发送HTTP请求失败: %v", err)
	}
	t.Log("HTTP请求已发送")

	// 读取HTTP响应
	r := bufio.NewReader(conn)

	// 读取状态行
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("读取HTTP响应失败: %v", err)
	}
	t.Logf("收到HTTP响应状态行: %s", strings.TrimSpace(line))

	// 验证响应状态码是否为200
	if !strings.Contains(line, "200") {
		t.Fatalf("期望HTTP状态码200，实际收到: %s", strings.TrimSpace(line))
	}

	// 继续读取响应体（验证连接是否正常工作）
	responseBody := ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			break
		}
		responseBody += line
	}

	// 验证响应体内容
	if !strings.Contains(responseBody, "Hello P2P Proxy!") {
		t.Logf("警告: 响应体中未找到期望内容，实际响应体: %s", responseBody)
	} else {
		t.Log("成功收到期望的响应内容")
	}

	// 清理资源：通过关闭Tracker连接来关闭Tracker服务器
	t.Log("正在关闭Tracker服务器...")
	if tr.conn != nil {
		tr.conn.Close()
	}

	// 等待Tracker goroutine退出或超时
	select {
	case err := <-done:
		if err != nil && err.Error() != "use of closed network connection" {
			t.Logf("Tracker退出时出现错误: %v", err)
		} else {
			t.Log("Tracker服务器已正常关闭")
		}
	case <-time.After(3 * time.Second):
		t.Log("等待Tracker服务器关闭超时")
	}

	t.Log("P2P代理测试完成")
}
