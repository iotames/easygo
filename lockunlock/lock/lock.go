package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/iotames/easygo/lockunlock"
)

var AppVersion = "v0.0.1"

func main() {
	if len(os.Args) == 2 && strings.Contains(os.Args[1], "version") {
		fmt.Print("文件加密: ")
		fmt.Println(AppVersion)
		os.Exit(0)
	}
	key := []byte("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")

	// 检查密钥长度
	if len(key) != 32 {
		fmt.Println("错误: 密钥必须是32字节(AES-256)")
		os.Exit(1)
	}

	// 获取当前目录下的所有文件（不包括子目录）
	entries, err := os.ReadDir(".")
	if err != nil {
		fmt.Printf("读取目录失败: %v\n", err)
		os.Exit(1)
	}

	// 获取当前正在运行的可执行文件名
	executable, err := os.Executable()
	if err != nil {
		fmt.Printf("获取可执行文件名失败: %v\n", err)
		os.Exit(1)
	}
	execName := ""
	if fileInfo, err := os.Stat(executable); err == nil {
		execName = fileInfo.Name()
	}

	count := 0 // 记录处理的文件数量

	// 遍历所有条目
	for _, entry := range entries {
		// 只处理文件，跳过目录
		if entry.IsDir() {
			continue
		}

		// 获取文件名
		filename := entry.Name()

		// 跳过正在运行的可执行文件（避免加密自身）
		// filename == execName 替代 filename == os.Args[0]
		if filename == execName || filename == "." || filename == ".." {
			continue
		}

		// 加密文件
		if err := lockunlock.EncryptFile(filename, filename, key); err != nil {
			// 加密文件 encrypt.exe 失败: open encrypt.exe: The process cannot access the file because it is being used by another process.
			fmt.Printf("加密文件 %s 失败: %v\n", filename, err)
			// 继续处理其他文件，不退出
			continue
		}

		count++
		fmt.Printf("文件加密成功: %s\n", filename)
	}

	// 显示弹框提示
	lockunlock.ShowMessage("加密完成", fmt.Sprintf("总共加密了 %d 个文件", count))
}
