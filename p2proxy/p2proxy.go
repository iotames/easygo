package p2proxy

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
)

// 简单基于 UDP 的 tracker + node 原型实现

// ProtoMsg 是节点之间通过 UDP 交换的控制/数据消息（JSON 编码，数据字段使用 base64）
// Type: 消息类型，如 register(注册)、lookup(查找)、stream_open(打开数据流)等
// From: 发送方节点ID
// To: 接收方节点ID（主要用于lookup消息）
// Addr: 节点地址信息（主要用于tracker通知）
// StreamID: 数据流标识符，用于标识一个特定的数据传输通道
// Target: 目标服务器地址(host:port格式)
// Data: base64编码的数据载荷
type ProtoMsg struct {
	Type     string `json:"type"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Addr     string `json:"addr,omitempty"`      // 用于 tracker 通知
	StreamID string `json:"stream_id,omitempty"` // 用于数据流标识
	Target   string `json:"target,omitempty"`    // 目标服务器 address host:port
	Data     string `json:"data,omitempty"`      // base64 编码的 payload
}

// Tracker: 在公网服务器上运行，接受节点注册并互相交换地址用于 UDP 打洞
// Tracker 是整个 P2P 网络的核心协调者，负责帮助各个节点发现彼此的网络地址
// ListenAddr: Tracker 监听的 UDP 地址
// conn: Tracker 的 UDP 连接
// mu: 用于保护 nodes 映射的互斥锁
// nodes: 存储已注册节点的 ID 到其网络地址的映射
type Tracker struct {
	ListenAddr string
	conn       *net.UDPConn
	mu         sync.Mutex
	nodes      map[string]*net.UDPAddr
}

// NewTracker 创建一个新的 Tracker 实例
// listenAddr: Tracker 监听的 UDP 地址
func NewTracker(listenAddr string) *Tracker {
	return &Tracker{ListenAddr: listenAddr, nodes: make(map[string]*net.UDPAddr)}
}

// Run 启动 Tracker 服务，开始监听和处理来自节点的消息
// 该方法会持续运行，处理节点的注册和地址查询请求
func (t *Tracker) Run() error {
	// 解析并监听指定的 UDP 地址
	udpAddr, err := net.ResolveUDPAddr("udp", t.ListenAddr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}
	t.conn = conn
	log.Printf("tracker listening %s", t.ListenAddr)

	// 创建缓冲区用于接收UDP数据包
	buf := make([]byte, 65535)

	// 持续监听并处理来自节点的消息
	for {
		// 从UDP连接读取数据
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			// 如果是连接关闭的错误，直接退出循环
			if strings.Contains(err.Error(), "closed") {
				log.Printf("tracker shutting down...")
				return nil
			}
			log.Printf("tracker read error: %v", err)
			continue
		}

		// 解析收到的JSON消息
		var m ProtoMsg
		if err := json.Unmarshal(buf[:n], &m); err != nil {
			log.Printf("tracker: invalid json from %s: %v", addr, err)
			continue
		}

		// 根据消息类型进行处理
		switch m.Type {
		case "register":
			// 处理节点注册请求
			// 将节点ID与其网络地址关联存储
			t.mu.Lock()
			t.nodes[m.From] = addr
			t.mu.Unlock()
			log.Printf("registered %s -> %s", m.From, addr.String())

			// 回复注册确认消息
			resp := ProtoMsg{Type: "registered"}
			b, _ := json.Marshal(&resp)
			conn.WriteToUDP(b, addr)

		case "lookup":
			// 处理节点地址查询请求
			t.mu.Lock()
			// 查找目标节点的地址
			peerAddr, ok := t.nodes[m.To]
			t.mu.Unlock()

			if ok {
				// 如果找到目标节点，回复其地址给请求方
				resp := ProtoMsg{Type: "peer", From: m.To, Addr: peerAddr.String()}
				b, _ := json.Marshal(&resp)
				conn.WriteToUDP(b, addr)

				// 同时通知目标节点有关请求方的信息，帮助双向NAT打洞
				t.mu.Lock()
				requesterAddr := t.nodes[m.From]
				t.mu.Unlock()
				if requesterAddr != nil {
					notify := ProtoMsg{Type: "notify", From: m.From, Addr: requesterAddr.String()}
					nb, _ := json.Marshal(&notify)
					conn.WriteToUDP(nb, peerAddr)
				}
			} else {
				// 如果未找到目标节点，回复未找到消息
				resp := ProtoMsg{Type: "notfound", To: m.To}
				b, _ := json.Marshal(&resp)
				conn.WriteToUDP(b, addr)
			}

		default:
			// 处理未知类型的消息
			log.Printf("tracker: unknown type %s from %s", m.Type, addr)
		}
	}
}

// Node: 代表运行在 NAT/内网的节点
// Node 是P2P网络中的参与者，可以发起连接请求或作为中继转发数据
// ID: 节点唯一标识符
// TrackerAddr: Tracker服务器的UDP地址
// conn: 节点的UDP连接
// mu: 用于保护 peers、streams 和 ready 映射的互斥锁
// peers: 存储已知其他节点的ID到其网络地址的映射
// streams: 存储数据流ID到本地TCP连接的映射（用于SOCKS代理侧）
// ready: 存储数据流ID到就绪信号通道的映射
type Node struct {
	ID          string
	TrackerAddr *net.UDPAddr
	conn        *net.UDPConn
	mu          sync.Mutex
	peers       map[string]*net.UDPAddr  // id -> addr
	streams     map[string]net.Conn      // streamID -> local TCP conn (for SOCKS side)
	ready       map[string]chan struct{} // streamID -> ready signal
}

// NewNode 创建一个新的节点实例
// id: 节点ID
// tracker: Tracker服务器地址
func NewNode(id string, tracker string) (*Node, error) {
	// 解析Tracker地址
	taddr, err := net.ResolveUDPAddr("udp", tracker)
	if err != nil {
		return nil, err
	}

	// 在本地随机端口创建UDP连接
	laddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, err
	}

	// 初始化节点并启动消息读取循环
	n := &Node{
		ID:          id,
		TrackerAddr: taddr,
		conn:        conn,
		peers:       make(map[string]*net.UDPAddr),
		streams:     make(map[string]net.Conn),
		ready:       make(map[string]chan struct{}),
	}

	// 启动异步消息读取循环
	go n.readLoop()
	return n, nil
}

// Close 关闭节点的UDP连接
func (n *Node) Close() {
	if n.conn != nil {
		n.conn.Close()
	}
}

// sendProto 发送ProtoMsg消息到指定地址
// addr: 目标地址
// m: 要发送的消息
func (n *Node) sendProto(addr *net.UDPAddr, m ProtoMsg) error {
	// 将消息序列化为JSON格式
	b, err := json.Marshal(&m)
	if err != nil {
		return err
	}

	// 通过UDP连接发送消息
	_, err = n.conn.WriteToUDP(b, addr)
	return err
}

// Register 向 tracker 注册自己的 ID 和地址信息
// 节点需要定期调用此方法以保持在Tracker中的注册状态
func (n *Node) Register() error {
	// 构造注册消息
	m := ProtoMsg{Type: "register", From: n.ID}

	// 发送注册消息到Tracker
	return n.sendProto(n.TrackerAddr, m)
}

// Lookup 向 tracker 请求指定 peer 节点的地址信息
// peerID: 要查找的节点ID
// 返回查找到的节点地址或错误信息
func (n *Node) Lookup(peerID string) (*net.UDPAddr, error) {
	// 构造查找消息
	m := ProtoMsg{Type: "lookup", From: n.ID, To: peerID}

	// 发送查找消息到Tracker
	if err := n.sendProto(n.TrackerAddr, m); err != nil {
		return nil, err
	}

	// 等待最多5秒以获取peer地址（通过readLoop处理返回的消息）
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		pa := n.peers[peerID]
		n.mu.Unlock()

		// 如果已获取到peer地址，立即返回
		if pa != nil {
			return pa, nil
		}

		// 短暂休眠后重试
		time.Sleep(100 * time.Millisecond)
	}

	// 超时未获取到peer地址
	return nil, errors.New("peer lookup timeout")
}

// readLoop 节点的消息读取循环，持续监听并处理来自其他节点或Tracker的消息
func (n *Node) readLoop() {
	// 创建缓冲区用于接收UDP数据包
	buf := make([]byte, 65535)

	// 持续监听并处理消息
	for {
		// 从UDP连接读取数据
		nread, addr, err := n.conn.ReadFromUDP(buf)
		if err != nil {
			// 处理临时网络错误
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			log.Printf("node read error: %v", err)
			return
		}

		// 解析收到的JSON消息
		var m ProtoMsg
		if err := json.Unmarshal(buf[:nread], &m); err != nil {
			log.Printf("node: invalid json from %s: %v", addr, err)
			continue
		}

		// 根据消息类型进行处理
		switch m.Type {
		case "registered":
			// 忽略Tracker的注册确认消息

		case "peer", "notify":
			// 来自Tracker的peer地址信息或通知消息
			// 更新本地peer地址映射
			if m.From != "" && m.Addr != "" {
				pa, err := net.ResolveUDPAddr("udp", m.Addr)
				if err == nil {
					n.mu.Lock()
					n.peers[m.From] = pa
					n.mu.Unlock()
					log.Printf("node %s learned peer %s -> %s", n.ID, m.From, pa)
				}
			}

		case "probe":
			// 探测包，记录对端地址以帮助NAT穿透
			// 探测包用于在正式通信前建立NAT映射关系
			if m.From != "" {
				n.mu.Lock()
				// 更新peer地址信息，可能比从tracker获取的更新
				n.peers[m.From] = addr
				n.mu.Unlock()
				log.Printf("node %s received probe from %s (%s)", n.ID, m.From, addr)
			}

		case "stream_open":
			// 对端请求我们代表它建立到目标服务器的 TCP 连接
			// 这是P2P代理的核心功能，由远端节点发起
			go n.handleStreamOpen(m, addr)

		case "stream_ready":
			// peer通知其已准备好接收/发送该数据流的数据
			// 这表示远端节点已成功连接到目标服务器
			if m.StreamID != "" {
				n.mu.Lock()
				ch := n.ready[m.StreamID]
				if ch != nil {
					// 关闭通道以通知等待方已就绪
					close(ch)
					delete(n.ready, m.StreamID)
				}
				n.mu.Unlock()
			}

		case "stream_data":
			// 数据转发消息：查找本地对应的数据流并写入数据
			// 这是从远端节点转发过来的目标服务器数据
			if m.StreamID != "" {
				n.mu.Lock()
				c := n.streams[m.StreamID]
				n.mu.Unlock()
				if c != nil {
					// 解码base64数据并写入本地连接
					data, err := base64.StdEncoding.DecodeString(m.Data)
					if err == nil {
						// 如果写入失败，需要清理对应的 stream，避免后续写入到已关闭连接导致 RST
						if _, werr := c.Write(data); werr != nil {
							log.Printf("write to local conn error: %v, cleaning stream %s", werr, m.StreamID)
							n.mu.Lock()
							delete(n.streams, m.StreamID)
							n.mu.Unlock()
							// 优雅关闭写端，避免触发 RST
							closeConnWrite(c)
						}
					}
				}
			}

		case "stream_close":
			// 远端节点通知关闭数据流
			if m.StreamID != "" {
				n.mu.Lock()
				c := n.streams[m.StreamID]
				delete(n.streams, m.StreamID)
				n.mu.Unlock()
				if c != nil {
					// 优雅地半关闭写端，让对端能优雅结束读操作
					closeConnWrite(c)
				}
			}

		default:
			// 处理未知类型的消息
			log.Printf("node %s got unknown msg type %s", n.ID, m.Type)
		}
	}
}

// handleStreamOpen 处理远端节点发起的连接请求
// m: 包含目标地址和数据流ID的请求消息
// fromAddr: 请求方的网络地址
func (n *Node) handleStreamOpen(m ProtoMsg, fromAddr *net.UDPAddr) {
	log.Printf("node %s received stream_open request from %s for target %s with stream_id %s",
		n.ID, m.From, m.Target, m.StreamID)

	// 检查必要参数
	if m.Target == "" || m.StreamID == "" {
		log.Printf("invalid stream_open request: missing target or stream_id")
		return
	}

	// 连接到目标服务器
	log.Printf("node %s: opening stream %s to target %s for peer %s", n.ID, m.StreamID, m.Target, m.From)
	c, err := net.Dial("tcp", m.Target)
	if err != nil {
		log.Printf("failed connect to target %s: %v", m.Target, err)
		// 连接失败，通知远端节点
		closeMsg := ProtoMsg{Type: "stream_close", From: n.ID, StreamID: m.StreamID}
		if sendErr := n.sendProto(fromAddr, closeMsg); sendErr != nil {
			log.Printf("failed to send stream_close: %v", sendErr)
		}
		return
	}
	log.Printf("successfully connected to target %s for stream %s", m.Target, m.StreamID)

	// 存储数据流与本地TCP连接的映射关系
	n.mu.Lock()
	n.streams[m.StreamID] = c
	n.mu.Unlock()

	// 启动goroutine从目标服务器读取数据并转发给远端节点
	go func() {
		br := bufio.NewReader(c)
		buf := make([]byte, 4096)
		for {
			// 从目标服务器读取数据
			nr, err := br.Read(buf)
			if nr > 0 {
				// 将数据编码为base64并发送给远端节点
				enc := base64.StdEncoding.EncodeToString(buf[:nr])
				dm := ProtoMsg{Type: "stream_data", From: n.ID, StreamID: m.StreamID, Data: enc}
				if err := n.sendProto(fromAddr, dm); err != nil {
					log.Printf("sendProto error: %v", err)
					break
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("read target error: %v", err)
				}
				// 读取结束或出错，通知远端节点关闭数据流
				cm := ProtoMsg{Type: "stream_close", From: n.ID, StreamID: m.StreamID}
				n.sendProto(fromAddr, cm)

				// 清理本地流映射并优雅关闭目标连接（只做一次删除）
				n.mu.Lock()
				delete(n.streams, m.StreamID)
				n.mu.Unlock()

				// 优雅半关闭写端，避免触发 RST
				closeConnWrite(c)
				return
			}
		}
	}()

	// 通知发起方节点已准备好接收数据
	readyMsg := ProtoMsg{Type: "stream_ready", From: n.ID, StreamID: m.StreamID}
	log.Printf("sending stream_ready to %s for stream %s", m.From, m.StreamID)
	if err := n.sendProto(fromAddr, readyMsg); err != nil {
		log.Printf("failed to send stream_ready message to %s: %v", fromAddr, err)
	}
}

// StartSocks5 在本地监听一个简单的 SOCKS5（仅支持无认证的 CONNECT），并把连接流量通过 peerID 的远端节点转发
// listenAddr: 本地SOCKS5代理监听地址
// peerID: 用于转发流量的远端节点ID
func (n *Node) StartSocks5(listenAddr string, peerID string) error {
	// 创建TCP监听器
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	log.Printf("socks5 listening %s (forward via %s)", listenAddr, peerID)

	// 启动异步处理循环
	go func() {
		for {
			// 接受客户端连接
			c, err := ln.Accept()
			if err != nil {
				log.Printf("socks accept error: %v", err)
				continue
			}
			// 为每个连接启动一个处理goroutine
			go n.handleSocksConn(c, peerID)
		}
	}()
	return nil
}

// handleSocksConn 处理来自SOCKS5客户端的连接请求
// c: 客户端连接
// peerID: 用于转发流量的远端节点ID
func (n *Node) handleSocksConn(c net.Conn, peerID string) {
	// SOCKS5握手阶段
	// 读取版本号、方法数和方法列表
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		log.Printf("socks handshake read error: %v", err)
		return
	}
	ver := hdr[0]
	nmethods := int(hdr[1])

	// 检查SOCKS版本（只支持SOCKS5）
	if ver != 0x05 {
		log.Printf("unsupported socks ver: %d (this server only supports SOCKS5)", ver)
		// 尝试处理可能是HTTP代理的请求
		if ver == 'G' || ver == 'P' || ver == 'H' { // GET, POST, PUT, HEAD, etc.
			log.Printf("this appears to be an HTTP request, not a SOCKS request - please configure your browser to use SOCKS5 proxy")
		} else if ver == 0x04 {
			log.Printf("this appears to be a SOCKS4 request, not SOCKS5 - this server only supports SOCKS5")
		}
		return
	}

	// 读取客户端支持的认证方法
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(c, methods); err != nil {
		log.Printf("socks methods read error: %v", err)
		return
	}

	// 回复选择无认证方法（0x00）
	c.Write([]byte{0x05, 0x00})

	// 读取客户端请求
	reqHdr := make([]byte, 4)
	if _, err := io.ReadFull(c, reqHdr); err != nil {
		log.Printf("socks req hdr err: %v", err)
		return
	}

	// 检查命令类型（只支持CONNECT）
	if reqHdr[1] != 0x01 { // CONNECT
		log.Printf("socks only support CONNECT")
		return
	}

	// 获取地址类型
	atyp := reqHdr[3]
	var dstAddr string

	// 根据地址类型解析目标地址
	switch atyp {
	case 0x01: // IPv4地址
		addr := make([]byte, 4)
		if _, err := io.ReadFull(c, addr); err != nil {
			return
		}
		portb := make([]byte, 2)
		if _, err := io.ReadFull(c, portb); err != nil {
			return
		}
		dstAddr = fmt.Sprintf("%d.%d.%d.%d:%d", addr[0], addr[1], addr[2], addr[3], int(portb[0])<<8|int(portb[1]))

	case 0x03: // 域名地址
		lbuf := make([]byte, 1)
		if _, err := io.ReadFull(c, lbuf); err != nil {
			return
		}
		l := int(lbuf[0])
		host := make([]byte, l)
		if _, err := io.ReadFull(c, host); err != nil {
			return
		}
		portb := make([]byte, 2)
		if _, err := io.ReadFull(c, portb); err != nil {
			return
		}
		dstAddr = fmt.Sprintf("%s:%d", string(host), int(portb[0])<<8|int(portb[1]))

	case 0x04: // IPv6地址
		addr := make([]byte, 16)
		if _, err := io.ReadFull(c, addr); err != nil {
			return
		}
		portb := make([]byte, 2)
		if _, err := io.ReadFull(c, portb); err != nil {
			return
		}
		ip := net.IP(addr)
		dstAddr = fmt.Sprintf("[%s]:%d", ip.String(), int(portb[0])<<8|int(portb[1]))

	default:
		log.Printf("unsupported atyp %d", atyp)
		return
	}

	// 回复连接成功的SOCKS响应
	// BND.ADDR和BND.PORT设置为零
	// 注意：对于IPv6，我们需要调整响应中的ATYP字段
	var reply []byte
	if atyp == 0x04 { // IPv6
		reply = []byte{0x05, 0x00, 0x00, 0x04, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	} else { // IPv4 or domain
		reply = []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	}
	c.Write(reply)

	// 通过Tracker获取远端节点地址
	peerAddr, err := n.Lookup(peerID)
	if err != nil {
		log.Printf("lookup peer %s failed: %v", peerID, err)
		return
	}

	// 发送探测包帮助 NAT 穿透，增加尝试次数
	log.Printf("sending probe packets to help NAT traversal")
	for i := 0; i < 10; i++ { // 增加探测包数量
		err := n.sendProto(peerAddr, ProtoMsg{Type: "probe", From: n.ID})
		if err != nil {
			log.Printf("failed to send probe packet %d: %v", i, err)
		}
		time.Sleep(50 * time.Millisecond) // 缩短间隔
	}
	log.Printf("sent 10 probe packets, waiting for NAT to stabilize")
	time.Sleep(1 * time.Second) // 增加等待时间

	// 创建数据流ID
	sid := fmt.Sprintf("%d", rand.Int63())

	// 存储本地连接与数据流的映射关系
	n.mu.Lock()
	n.streams[sid] = c
	// 创建就绪信号通道并等待远端节点准备就绪
	ch := make(chan struct{})
	n.ready[sid] = ch
	n.mu.Unlock()

	// 向远端节点发送连接请求
	open := ProtoMsg{Type: "stream_open", From: n.ID, StreamID: sid, Target: dstAddr}
	log.Printf("sending stream_open request to peer %s for target %s", peerID, dstAddr)
	if err := n.sendProto(peerAddr, open); err != nil {
		log.Printf("send stream_open error: %v", err)
		n.mu.Lock()
		delete(n.ready, sid)
		delete(n.streams, sid)
		n.mu.Unlock()
		return
	}

	// 等待远端节点准备就绪（最多等待15秒）
	log.Printf("waiting for stream_ready from peer (timeout: 15s)")
	select {
	case <-ch:
		log.Printf("received stream_ready, connection established")
		// 远端节点已准备就绪

	case <-time.After(15 * time.Second): // 增加超时时间到15秒
		log.Printf("warning: timed out waiting for stream_ready for %s, this usually means NAT hole punching failed", sid)
		log.Printf("diagnostic info:")
		log.Printf("  - Peer address: %s", peerAddr.String())
		log.Printf("  - Local UDP address: %s", n.conn.LocalAddr().String())
		log.Printf("  - This may be caused by strict NAT/firewall settings")
		log.Printf("tip: try placing one node on a public IP, or configure your firewall/NAT to allow UDP traffic")

		// 清理资源
		n.mu.Lock()
		delete(n.ready, sid)
		delete(n.streams, sid)
		n.mu.Unlock()
		return
	}

	// 启动goroutine从本地SOCKS客户端读取数据并转发给远端节点
	go func() {
		buf := make([]byte, 4096)
		for {
			// 从本地客户端读取数据
			nr, err := c.Read(buf)
			if nr > 0 {
				// 将数据编码为base64并发送给远端节点
				enc := base64.StdEncoding.EncodeToString(buf[:nr])
				dm := ProtoMsg{Type: "stream_data", From: n.ID, StreamID: sid, Data: enc}
				if err := n.sendProto(peerAddr, dm); err != nil {
					log.Printf("sendProto error: %v", err)
					break
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("local read err: %v", err)
				}
				// 读取结束，通知远端节点关闭数据流
				cm := ProtoMsg{Type: "stream_close", From: n.ID, StreamID: sid}
				n.sendProto(peerAddr, cm)
				n.mu.Lock()
				delete(n.streams, sid)
				n.mu.Unlock()
				// 优雅关闭写端，避免触发 RST 给对端
				closeConnWrite(c)
				return
			}
		}
	}()

	// 数据流的写入端（从远端节点到本地）由readLoop处理，它会写入到n.streams[sid]连接中
}

// 新增：优雅地关闭连接的写端，优先使用 TCP 的 CloseWrite，避免触发 RST
func closeConnWrite(c net.Conn) {
	if c == nil {
		return
	}
	if tc, ok := c.(*net.TCPConn); ok {
		// 忽略错误，尽力半关闭写端
		_ = tc.CloseWrite()
		return
	}
	// 回退到完全关闭
	_ = c.Close()
}

// Close 优雅关闭 Tracker
func (t *Tracker) Close() error {
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}
