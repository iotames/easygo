package main

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

type ClientInfo struct {
	Addr     *net.UDPAddr
	LastSeen time.Time
}

var clients = make(map[string]*ClientInfo)
var clientsMutex sync.RWMutex

func main() {
	fmt.Println("启动信令服务器在 :929")
	addr, _ := net.ResolveUDPAddr("udp", ":929")
	conn, _ := net.ListenUDP("udp", addr)
	defer conn.Close()

	// 启动客户端清理协程
	go cleanUpClients(conn)

	buffer := make([]byte, 1024)

	for {
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Printf("读取错误: %v\n", err)
			continue
		}

		msg := string(buffer[:n])
		fmt.Printf("收到来自 %s 的消息: %s\n", clientAddr.String(), msg)

		parts := strings.SplitN(msg, " ", 2)
		if len(parts) < 2 {
			continue
		}

		switch parts[0] {
		case "REGISTER":
			clientID := parts[1]
			clientsMutex.Lock()
			clients[clientID] = &ClientInfo{
				Addr:     clientAddr,
				LastSeen: time.Now(),
			}
			clientsMutex.Unlock()
			fmt.Printf("客户端 %s 注册成功, 地址: %s\n", clientID, clientAddr.String())
			conn.WriteToUDP([]byte("REGISTER_OK"), clientAddr)

		case "CONNECT":
			targetID := parts[1]
			clientsMutex.Lock()
			targetInfo, exists := clients[targetID]
			if !exists {
				clientsMutex.Unlock()
				conn.WriteToUDP([]byte("ERROR: Target not found"), clientAddr)
				continue
			}
			// 更新目标客户端的上次活动时间
			targetInfo.LastSeen = time.Now()
			// 复制地址信息，以便在锁外使用
			targetAddr := &net.UDPAddr{
				IP:   targetInfo.Addr.IP,
				Port: targetInfo.Addr.Port,
			}
			clientsMutex.Unlock()

			// 交换地址信息
			responseToTarget := fmt.Sprintf("PEER_INFO %s %d", clientAddr.IP, clientAddr.Port)
			conn.WriteToUDP([]byte(responseToTarget), targetAddr)

			responseToRequester := fmt.Sprintf("PEER_INFO %s %d", targetAddr.IP, targetAddr.Port)
			conn.WriteToUDP([]byte(responseToRequester), clientAddr)
			fmt.Printf("已交换 %s 和 %s 的地址信息\n", clientAddr.String(), targetAddr.String())

		case "HEARTBEAT":
			clientID := parts[1]
			clientsMutex.Lock()
			if info, exists := clients[clientID]; exists {
				info.LastSeen = time.Now()
			}
			clientsMutex.Unlock()
			conn.WriteToUDP([]byte("HEARTBEAT_ACK"), clientAddr)
		}
	}
}

// 清理不活动的客户端
func cleanUpClients(conn *net.UDPConn) {
	for {
		time.Sleep(300 * time.Second)
		clientsMutex.Lock()
		now := time.Now()
		for id, info := range clients {
			if now.Sub(info.LastSeen) > 3600*time.Second {
				fmt.Printf("客户端 %s 超时未活动，已移除\n", id)
				delete(clients, id)
			}
		}
		clientsMutex.Unlock()
	}
}
