## 简介

Go语言编写的跨平台网络监控程序：监控实时流量，包括出站和入站流量，主机地址，端口号，进程名，PID，流量统计等。


## 依赖

本系统依赖第三方包：`github.com/google/gopacket`，而 `gopacket` 项目依赖底层的 C 库来实现核心的抓包功能。

- Windows依赖：[WinPcap](https://www.winpcap.org/install/) 或 [Npcap](https://nmap.org/npcap/)。
- Linux依赖：`libpcap`。包管理器可尝试安装 `libpcap-dev`。

### Windows

Windows缺少依赖库可能报错： `couldn't load wpcap.dll`。

访问 Npcap 官方网站 (https://nmap.org/npcap/) 下载最新的安装包。

运行安装程序时，为确保兼容性，建议勾选 `Install Npcap in WinPcap API-compatible Mode` 选项。这能确保系统同时存在 `WinPcap` 和 `Npcap` 所需的接口。

### Linux

被第三方程序使用的依赖文件，前面的Windows系统叫动态链接库（DLL），文件后缀 为`.dll`，在Linux则称为共享对象‌（Shared Object），文件后缀为 `.so‌`

1. 安装动态链接库（共享对象）：会在系统的共享库路径下生成 `.so` 文件。

```bash
# Debian/Ubuntu 系统
# 查找包含依赖库的安装包
apt search libpcap-dev
apt search libpcap
# 如果找到包，则安装
apt install libpcap-dev

# RHEL/CentOS 系统
yum install libpcap-devel
```

2. 使用静态链接独立编译

在Linux系统，可使用静态链接，编译出不依赖 `libpcap.so` 动态链接库文件的独立程序。

```bash
CGO_LDFLAGS="-L/path/to/static/libpcap -lpcap -static" go build your_program.go
```

### 验证

```go
package main

import (
    "fmt"
    "github.com/google/gopacket/pcap"
)

func main() {
    version := pcap.Version()
    fmt.Println(version)
}
```

### 其他

一个容易被忽略的点是，就算代码没有直接使用C库，Go语言的部分标准库（如 net、os/user等）在某些情况下也会在底层使用C的实现。
在默认设置（`CGO_ENABLED=1`）下编译这类程序，生成的可执行文件仍可能依赖系统的C库（如glibc）。这时，设置 `CGO_ENABLED=0` 会强制这些标准库使用其纯Go的实现版本，从而避免对C库的依赖，实现真正的静态链接。
但这仅限于标准库，对于依赖C库的第三方包则无能为力。

```bash
# 方法一：直接静态链接指定的libpcap库
# 可能仍然依赖系统的动态C库（如glibc），除非所有依赖都是静态的
​​要求​​：需要预先编译好静态版libpcap（libpcap.a）
CGO_LDFLAGS="-L/path/to/static/libpcap -lpcap -static" go build your_program.go

# 方法二：使用musl C库（静态友好）替代系统glibc，实现完全静态链接
# 生成真正独立的可执行文件，不依赖任何系统so文件
# 要求​​：需要安装musl工具链
CC=musl-gcc CGO_ENABLED=1 go build -ldflags="-extldflags -static" -o myapp .
```

安装musl工具链：

```bash
# Debian/Ubuntu
sudo apt install musl-tools

# RHEL/CentOS
sudo yum install musl-gcc
```

完整编译流程示例：

```bash
# 1. 安装依赖
# musl仅是替代系统的glibc标准库，避免程序依赖系统动态C库，但第三方库（如libpcap）仍需单独静态链接。
# apt install libpcap-dev
sudo apt install musl-tools

# 2. 编译（使用musl静态链接）
CC=musl-gcc CGO_ENABLED=1 go build -ldflags="-extldflags -static" -o myapp .

# 3. 验证是否静态
file myapp  # 应显示"statically linked"
ldd myapp  # 应显示"not a dynamic executable"
```

关于C语言的编译参数：`-L/path/to/static/libpcap -lpcap -static`

1. `-lpcap`: 在编译时，告诉连接器：程序需要libpcap的功能​​。但​​不决定​​是静态链接还是动态链接。链接器会查找名为 libpcap.a（静态库）或 libpcap.so（动态库）的文件。结合 -L使用，链接器会在 -L指定的路径中优先查找。

2. `-L/path/to/static/libpcap`​​：指定链接器搜索库文件的目录路径。例如，如果静态库 libpcap.a存放在 /usr/local/lib，则用 `-L/usr/local/lib` 告诉链接器到该目录查找库文件。

3. `-static`: 强制链接器使用静态链接，而不是动态链接。

示例：

```bash
# -L/usr/local/lib -lpcap表示：在 /usr/local/lib 目录下查找 libpcap.a 并链接。
CGO_ENABLED=1 CC=musl-gcc CGO_LDFLAGS="-L/usr/local/lib -lpcap" go build -ldflags="-extldflags -static" -o myapp .

# gcc编译器，即使有-lpcap，也默认使用动态链接。
gcc -o program program.o -lpcap
# 静态链接，需要使用-static参数显式指定。
gcc -static -o program program.o -lpcap
```

结论：对于在Linux系统编译不依赖libpcap.so的独立程序，​​推荐使用musl方案​​，它是Linux的C标准库替代品​​，用于替代glibc。可以更可靠地实现完全静态编译。Windows/macOS不使用musl​​，它们有各自的C库生态。
