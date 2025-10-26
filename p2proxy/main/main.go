package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iotames/easygo/p2proxy"
)

func main() {
	mode := flag.String("mode", "node", "mode: tracker or node")
	listen := flag.String("listen", ":40000", "tracker listen address (udp)")
	id := flag.String("id", "node1", "node id")
	trackerAddr := flag.String("tracker", "127.0.0.1:40000", "tracker udp addr")
	socks := flag.String("socks", "", "start local socks5 listen address, e.g. 127.0.0.1:1080")
	peer := flag.String("peer", "", "default peer id to forward socks connections to")
	flag.Parse()

	if *mode == "tracker" {
		t := p2proxy.NewTracker(*listen)
		go func() {
			if err := t.Run(); err != nil {
				log.Fatalf("tracker run error: %v", err)
			}
		}()
		// wait for ctrl-c
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Printf("tracker shutting down")
		return
	}

	// node mode
	n, err := p2proxy.NewNode(*id, *trackerAddr)
	if err != nil {
		log.Fatalf("new node error: %v", err)
	}
	defer n.Close()
	// register periodically
	if err := n.Register(); err != nil {
		log.Printf("register error: %v", err)
	}
	// re-register every 120s
	// 每两分钟重新注册，防止连接意外断开
	go func() {
		ticker := time.NewTicker(120 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			n.Register()
		}
	}()

	if *socks != "" {
		if *peer == "" {
			log.Fatalf("when using socks mode you must set -peer to the remote node id to forward to")
		}
		if err := n.StartSocks5(*socks, *peer); err != nil {
			log.Fatalf("start socks error: %v", err)
		}
	}

	log.Printf("node %s running (tracker=%s)", *id, *trackerAddr)
	// wait for ctrl-c
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("node shutting down")
}
