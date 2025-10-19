package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("用法: p2p_client [服务器地址] [客户端ID] [要连接的客户端ID]")
		fmt.Println("例如: p2p_client 1.2.3.4:929 clientA clientB")
		return
	}

	serverAddrStr := os.Args[1]
	myID := os.Args[2]
	targetID := os.Args[3]

	// 解析信令服务器地址
	serverAddr, err := net.ResolveUDPAddr("udp", serverAddrStr)
	if err != nil {
		panic(err)
	}

	// 创建本地UDP Socket，端口随机 - 使用 ListenUDP 而不是 DialUDP
	localAddr, _ := net.ResolveUDPAddr("udp", ":0")
	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	fmt.Printf("本地地址: %s\n", conn.LocalAddr().String())

	// 注册到信令服务器
	registerMsg := fmt.Sprintf("REGISTER %s", myID)
	_, err = conn.WriteToUDP([]byte(registerMsg), serverAddr)
	if err != nil {
		panic(err)
	}

	buffer := make([]byte, 1024)
	n, addr, err := conn.ReadFromUDP(buffer)
	if err != nil {
		panic(err)
	}

	// 检查消息是否来自信令服务器
	if !addr.IP.Equal(serverAddr.IP) || addr.Port != serverAddr.Port {
		fmt.Printf("收到非信令服务器的消息，忽略: %s\n", addr.String())
	} else {
		if string(buffer[:n]) != "REGISTER_OK" {
			fmt.Println("注册失败")
			return
		}
		fmt.Println("注册成功")
	}

	// 请求连接目标客户端
	connectMsg := fmt.Sprintf("CONNECT %s", targetID)
	_, err = conn.WriteToUDP([]byte(connectMsg), serverAddr)
	if err != nil {
		panic(err)
	}

	// 读取服务器返回的对等端地址信息
	n, addr, err = conn.ReadFromUDP(buffer)
	if err != nil {
		panic(err)
	}

	// 确保消息来自信令服务器
	if !addr.IP.Equal(serverAddr.IP) || addr.Port != serverAddr.Port {
		fmt.Printf("收到非信令服务器的消息，忽略: %s\n", addr.String())
		return
	}

	peerInfo := string(buffer[:n])
	if strings.HasPrefix(peerInfo, "ERROR") {
		fmt.Println(peerInfo)
		return
	}

	// 解析对等端的IP和端口
	infoParts := strings.Split(peerInfo, " ")
	if len(infoParts) != 3 || infoParts[0] != "PEER_INFO" {
		fmt.Printf("无效的对等端信息: %s\n", peerInfo)
		return
	}
	peerIP := infoParts[1]
	peerPort, _ := strconv.Atoi(infoParts[2])
	peerAddr := &net.UDPAddr{
		IP:   net.ParseIP(peerIP),
		Port: peerPort,
	}
	fmt.Printf("对等端地址: %s\n", peerAddr.String())

	// 开始打洞：向对等端的公网地址发送UDP包
	fmt.Println("开始向对等端地址发送UDP包进行打洞...")
	go func() {
		for {
			// 持续发送，维持NAT映射
			_, err := conn.WriteToUDP([]byte("PUNCH"), peerAddr)
			if err != nil {
				fmt.Printf("发送打洞包错误: %v\n", err)
			}
			time.Sleep(5 * time.Second) // 每5秒发送一次维持包
		}
	}()

	// 处理可能从对等端发来的消息
	fmt.Println("开始监听来自对等端的消息...")
	go func() {
		for {
			n, addr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				fmt.Printf("读取错误: %v\n", err)
				continue
			}
			// 忽略来自信令服务器的消息
			if addr.IP.Equal(serverAddr.IP) && addr.Port == serverAddr.Port {
				continue
			}
			msg := string(buffer[:n])
			if msg == "PUNCH" {
				fmt.Printf("收到来自 %s 的打洞包，P2P连接可能已建立！\n", addr.String())
			} else {
				fmt.Printf("收到来自 %s 的消息: %s\n", addr.String(), msg)
			}
		}
	}()

	// 简单的控制台输入，用于发送测试消息
	fmt.Printf("输入消息直接发送给对等端%s (输入exit退出):\n", peerAddr.String())
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "exit" {
			break
		}
		_, err := conn.WriteToUDP([]byte(text), peerAddr)
		if err != nil {
			fmt.Printf("发送消息错误: %v\n", err)
		}
	}
}
