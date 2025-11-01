package main

import (
	"fmt"
	"os"
)

var AppVersion = "v0.1.1"

func main() {
	if version {
		fmt.Printf("lockunlock: %s", AppVersion)
		os.Exit(0)
	}
	// 检查密钥长度
	if len(key) != 32 {
		fmt.Printf("key错误：AES-256密钥必须是32字节")
		os.Exit(1)
	}
	if err := lockopt(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	parseCmdArgs()
}
