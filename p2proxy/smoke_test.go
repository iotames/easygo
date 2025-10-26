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

// helper: get free UDP port
func freeUDPPort() (int, error) {
	l, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.LocalAddr().(*net.UDPAddr).Port, nil
}

// helper: get free TCP port
func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func TestSmokeP2Proxy(t *testing.T) {
	// find a free UDP port for tracker
	tp, err := freeUDPPort()
	if err != nil {
		t.Fatalf("freeUDPPort: %v", err)
	}
	trackerAddr := fmt.Sprintf("127.0.0.1:%d", tp)

	// start tracker
	tr := NewTracker(trackerAddr)
	done := make(chan error, 1)
	go func() {
		done <- tr.Run()
	}()

	// wait for tracker to bind
	deadline := time.Now().Add(2 * time.Second)
	for tr.conn == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tr.conn == nil {
		t.Fatalf("tracker did not start in time")
	}

	// start nodeB
	nb, err := NewNode("nodeB", trackerAddr)
	if err != nil {
		t.Fatalf("NewNode nodeB: %v", err)
	}
	defer nb.Close()
	if err := nb.Register(); err != nil {
		t.Fatalf("nodeB Register: %v", err)
	}

	// start nodeA
	na, err := NewNode("nodeA", trackerAddr)
	if err != nil {
		t.Fatalf("NewNode nodeA: %v", err)
	}
	defer na.Close()
	if err := na.Register(); err != nil {
		t.Fatalf("nodeA Register: %v", err)
	}

	// pick a free TCP port for socks
	sp, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	socksAddr := fmt.Sprintf("127.0.0.1:%d", sp)
	if err := na.StartSocks5(socksAddr, "nodeB"); err != nil {
		t.Fatalf("StartSocks5: %v", err)
	}

	// ensure peer discovery
	peerAddr, err := na.Lookup("nodeB")
	if err != nil {
		t.Fatalf("Lookup nodeB failed: %v", err)
	}

	// send several small probe packets from both sides to help NAT punching
	for i := 0; i < 3; i++ {
		_ = na.sendProto(peerAddr, ProtoMsg{Type: "probe", From: na.ID})
		// try to get nb's peer addr too
		nbPeer, _ := nb.Lookup("nodeA")
		if nbPeer != nil {
			_ = nb.sendProto(nbPeer, ProtoMsg{Type: "probe", From: nb.ID})
		}
		time.Sleep(100 * time.Millisecond)
	}
	// give a moment for probe packets to traverse
	time.Sleep(200 * time.Millisecond)

	// start a local HTTP test server so the target is reliable and local
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	target := u.Host

	// use socks5 dialer to connect to target
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		t.Fatalf("socks5 dialer: %v", err)
	}

	conn, err := dialer.Dial("tcp", target)
	if err != nil {
		t.Fatalf("dial through socks failed: %v", err)
	}
	defer conn.Close()

	// write simple HTTP GET
	_, err = conn.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"))
	if err != nil {
		t.Fatalf("write http req: %v", err)
	}

	// read response headers
	r := bufio.NewReader(conn)
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !strings.Contains(line, "200") {
		t.Fatalf("unexpected response status line: %s", strings.TrimSpace(line))
	}

	// cleanup: close tracker by closing its conn
	if tr.conn != nil {
		tr.conn.Close()
	}
	// wait for tracker goroutine to exit or timeout
	select {
	case err := <-done:
		if err != nil && err.Error() != "use of closed network connection" {
			// it's okay if it closed due to our Close
			t.Logf("tracker exited: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Log("tracker did not exit quickly")
	}
}
