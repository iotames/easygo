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
	"sync"
	"time"
)

// 简单基于 UDP 的 tracker + node 原型实现

// ProtoMsg 是节点之间通过 UDP 交换的控制/数据消息（JSON 编码，数据字段使用 base64）
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
type Tracker struct {
	ListenAddr string
	conn       *net.UDPConn
	mu         sync.Mutex
	nodes      map[string]*net.UDPAddr
}

func NewTracker(listenAddr string) *Tracker {
	return &Tracker{ListenAddr: listenAddr, nodes: make(map[string]*net.UDPAddr)}
}

func (t *Tracker) Run() error {
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
	buf := make([]byte, 65535)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("tracker read error: %v", err)
			continue
		}
		var m ProtoMsg
		if err := json.Unmarshal(buf[:n], &m); err != nil {
			log.Printf("tracker: invalid json from %s: %v", addr, err)
			continue
		}
		switch m.Type {
		case "register":
			t.mu.Lock()
			t.nodes[m.From] = addr
			t.mu.Unlock()
			log.Printf("registered %s -> %s", m.From, addr.String())
			// reply ack
			resp := ProtoMsg{Type: "registered"}
			b, _ := json.Marshal(&resp)
			conn.WriteToUDP(b, addr)
		case "lookup":
			t.mu.Lock()
			peerAddr, ok := t.nodes[m.To]
			t.mu.Unlock()
			if ok {
				// reply to requester with peer address
				resp := ProtoMsg{Type: "peer", From: m.To, Addr: peerAddr.String()}
				b, _ := json.Marshal(&resp)
				conn.WriteToUDP(b, addr)
				// also notify peer about requester to help punching
				t.mu.Lock()
				requesterAddr := t.nodes[m.From]
				t.mu.Unlock()
				if requesterAddr != nil {
					notify := ProtoMsg{Type: "notify", From: m.From, Addr: requesterAddr.String()}
					nb, _ := json.Marshal(&notify)
					conn.WriteToUDP(nb, peerAddr)
				}
			} else {
				resp := ProtoMsg{Type: "notfound", To: m.To}
				b, _ := json.Marshal(&resp)
				conn.WriteToUDP(b, addr)
			}
		default:
			log.Printf("tracker: unknown type %s from %s", m.Type, addr)
		}
	}
}

// Node: 代表运行在 NAT/内网的节点
type Node struct {
	ID          string
	TrackerAddr *net.UDPAddr
	conn        *net.UDPConn
	mu          sync.Mutex
	peers       map[string]*net.UDPAddr  // id -> addr
	streams     map[string]net.Conn      // streamID -> local TCP conn (for SOCKS side)
	ready       map[string]chan struct{} // streamID -> ready signal
}

func NewNode(id string, tracker string) (*Node, error) {
	taddr, err := net.ResolveUDPAddr("udp", tracker)
	if err != nil {
		return nil, err
	}
	// local UDP on random port
	laddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, err
	}
	n := &Node{ID: id, TrackerAddr: taddr, conn: conn, peers: make(map[string]*net.UDPAddr), streams: make(map[string]net.Conn), ready: make(map[string]chan struct{})}
	go n.readLoop()
	return n, nil
}

func (n *Node) Close() {
	if n.conn != nil {
		n.conn.Close()
	}
}

func (n *Node) sendProto(addr *net.UDPAddr, m ProtoMsg) error {
	b, err := json.Marshal(&m)
	if err != nil {
		return err
	}
	_, err = n.conn.WriteToUDP(b, addr)
	return err
}

// Register 向 tracker 注册自己的 ID
func (n *Node) Register() error {
	m := ProtoMsg{Type: "register", From: n.ID}
	return n.sendProto(n.TrackerAddr, m)
}

// Lookup 向 tracker 请求 peer 地址
func (n *Node) Lookup(peerID string) (*net.UDPAddr, error) {
	m := ProtoMsg{Type: "lookup", From: n.ID, To: peerID}
	if err := n.sendProto(n.TrackerAddr, m); err != nil {
		return nil, err
	}
	// wait up to 5s for peer to be discovered (peer message handled in readLoop which sets n.peers)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		pa := n.peers[peerID]
		n.mu.Unlock()
		if pa != nil {
			return pa, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, errors.New("peer lookup timeout")
}

func (n *Node) readLoop() {
	buf := make([]byte, 65535)
	for {
		nread, addr, err := n.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			log.Printf("node read error: %v", err)
			return
		}
		var m ProtoMsg
		if err := json.Unmarshal(buf[:nread], &m); err != nil {
			log.Printf("node: invalid json from %s: %v", addr, err)
			continue
		}
		switch m.Type {
		case "registered":
			// ignore
		case "peer", "notify":
			//来自 tracker 的 peer 信息
			if m.From != "" && m.Addr != "" {
				pa, err := net.ResolveUDPAddr("udp", m.Addr)
				if err == nil {
					n.mu.Lock()
					n.peers[m.From] = pa
					n.mu.Unlock()
					log.Printf("node %s learned peer %s -> %s", n.ID, m.From, pa)
				}
			}
		case "stream_open":
			// 对端请求我们代表它建立到目标服务器的 TCP 连接
			go n.handleStreamOpen(m, addr)
		case "stream_ready":
			// peer notified that its side is ready to receive/send data for stream
			if m.StreamID != "" {
				n.mu.Lock()
				ch := n.ready[m.StreamID]
				if ch != nil {
					// close to signal readiness
					close(ch)
					delete(n.ready, m.StreamID)
				}
				n.mu.Unlock()
			}
		case "stream_data":
			// 数据转发：查找本地 stream
			if m.StreamID != "" {
				n.mu.Lock()
				c := n.streams[m.StreamID]
				n.mu.Unlock()
				if c != nil {
					data, err := base64.StdEncoding.DecodeString(m.Data)
					if err == nil {
						c.Write(data)
					}
				}
			}
		case "stream_close":
			if m.StreamID != "" {
				n.mu.Lock()
				c := n.streams[m.StreamID]
				delete(n.streams, m.StreamID)
				n.mu.Unlock()
				if c != nil {
					c.Close()
				}
			}
		default:
			log.Printf("node %s got unknown msg type %s", n.ID, m.Type)
		}
	}
}

func (n *Node) handleStreamOpen(m ProtoMsg, fromAddr *net.UDPAddr) {
	if m.Target == "" || m.StreamID == "" {
		return
	}
	// dial target
	log.Printf("node %s: opening stream %s to target %s for peer %s", n.ID, m.StreamID, m.Target, m.From)
	c, err := net.Dial("tcp", m.Target)
	if err != nil {
		log.Printf("failed connect to target %s: %v", m.Target, err)
		// send close to peer
		closeMsg := ProtoMsg{Type: "stream_close", From: n.ID, StreamID: m.StreamID}
		n.sendProto(fromAddr, closeMsg)
		return
	}
	// store stream
	n.mu.Lock()
	n.streams[m.StreamID] = c
	n.mu.Unlock()

	// start goroutine: read from target and send stream_data back to origin
	go func() {
		br := bufio.NewReader(c)
		buf := make([]byte, 4096)
		for {
			nr, err := br.Read(buf)
			if nr > 0 {
				enc := base64.StdEncoding.EncodeToString(buf[:nr])
				dm := ProtoMsg{Type: "stream_data", From: n.ID, StreamID: m.StreamID, Data: enc}
				if err := n.sendProto(fromAddr, dm); err != nil {
					log.Printf("sendProto error: %v", err)
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("read target error: %v", err)
				}
				// send close
				cm := ProtoMsg{Type: "stream_close", From: n.ID, StreamID: m.StreamID}
				n.sendProto(fromAddr, cm)
				n.mu.Lock()
				delete(n.streams, m.StreamID)
				n.mu.Unlock()
				n.mu.Lock()
				delete(n.streams, m.StreamID)
				n.mu.Unlock()
				c.Close()
				return
			}
		}
	}()
	// notify origin that we're ready to receive data for this stream
	readyMsg := ProtoMsg{Type: "stream_ready", From: n.ID, StreamID: m.StreamID}
	n.sendProto(fromAddr, readyMsg)
}

// StartSocks5 在本地监听一个简单的 SOCKS5（仅支持无认证的 CONNECT），并把连接流量通过 peerID 的远端节点转发
func (n *Node) StartSocks5(listenAddr string, peerID string) error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	log.Printf("socks5 listening %s (forward via %s)", listenAddr, peerID)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				log.Printf("socks accept error: %v", err)
				continue
			}
			go n.handleSocksConn(c, peerID)
		}
	}()
	return nil
}

func (n *Node) handleSocksConn(c net.Conn, peerID string) {
	defer c.Close()
	// very small SOCKS5 handshake implementation
	// Read VER, NMETHODS, METHODS
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		log.Printf("socks handshake read error: %v", err)
		return
	}
	ver := hdr[0]
	nmethods := int(hdr[1])
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
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(c, methods); err != nil {
		log.Printf("socks methods read error: %v", err)
		return
	}
	// reply: no auth
	c.Write([]byte{0x05, 0x00})

	// read request
	reqHdr := make([]byte, 4)
	if _, err := io.ReadFull(c, reqHdr); err != nil {
		log.Printf("socks req hdr err: %v", err)
		return
	}
	if reqHdr[1] != 0x01 { // CONNECT
		log.Printf("socks only support CONNECT")
		return
	}
	atyp := reqHdr[3]
	var dstAddr string
	switch atyp {
	case 0x01: // IPv4
		addr := make([]byte, 4)
		if _, err := io.ReadFull(c, addr); err != nil {
			return
		}
		portb := make([]byte, 2)
		if _, err := io.ReadFull(c, portb); err != nil {
			return
		}
		dstAddr = fmt.Sprintf("%d.%d.%d.%d:%d", addr[0], addr[1], addr[2], addr[3], int(portb[0])<<8|int(portb[1]))
	case 0x03: // domain
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
	default:
		log.Printf("unsupported atyp %d", atyp)
		return
	}

	// reply success to SOCKS client (we'll proxy)
	// BND.ADDR and BND.PORT set to zero
	c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// ensure we know peer address
	peerAddr, err := n.Lookup(peerID)
	if err != nil {
		log.Printf("lookup peer %s failed: %v", peerID, err)
		return
	}

	// create stream id
	sid := fmt.Sprintf("%d", rand.Int63())
	// store mapping
	n.mu.Lock()
	n.streams[sid] = c
	// create a ready channel and wait for peer to be ready
	ch := make(chan struct{})
	n.ready[sid] = ch
	n.mu.Unlock()

	// send stream_open to peer
	open := ProtoMsg{Type: "stream_open", From: n.ID, StreamID: sid, Target: dstAddr}
	if err := n.sendProto(peerAddr, open); err != nil {
		log.Printf("send stream_open error: %v", err)
		n.mu.Lock()
		delete(n.ready, sid)
		delete(n.streams, sid)
		n.mu.Unlock()
		return
	}

	// wait up to 3s for stream_ready
	select {
	case <-ch:
		// ready
	case <-time.After(3 * time.Second):
		log.Printf("warning: timed out waiting for stream_ready for %s", sid)
	}

	// read from local TCP and send stream_data messages
	go func() {
		buf := make([]byte, 4096)
		for {
			nr, err := c.Read(buf)
			if nr > 0 {
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
				cm := ProtoMsg{Type: "stream_close", From: n.ID, StreamID: sid}
				n.sendProto(peerAddr, cm)
				n.mu.Lock()
				delete(n.streams, sid)
				n.mu.Unlock()
				c.Close()
				return
			}
		}
	}()

	// The writing side (from peer -> local) is handled by readLoop that writes into n.streams[sid]'s connection
}
