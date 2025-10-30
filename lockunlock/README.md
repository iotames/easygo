## 简介

1. 加锁：把加锁工具，和要加锁的那些文件，放一个文件夹。双击加锁。
2. 解锁：把加锁工具拿出来。再把解锁工具放进去，双击解锁。

适用的加解密对象：包括但不限于图片，文本，word, excel，exe, 音频视频等......


## 注意

切记！！！！！

1. 加锁的时候，不要把解锁工具也放进去，不然解锁工具，也会被破坏。
2. 解锁的时候，不要把加锁工具也放一起，不然加锁工具，也会被破坏。


## 原理

计算机一切文件的底层都是二进制。
基于此，可以运用数学密码学算法，对任意私有文件，进行加密解密。
包括但不限于图片，文本，word, excel，exe, 音频视频等......

加密：先将文件字节数组首尾颠倒，然后每隔37个字节插入随机17个节。最后进行AES-256加密。
解密：对上面步骤进行逆序。先进行AES-256解密，然后每隔37个字节删除17个节，最后将文件字节数组首尾颠倒。


## Windows下编译

```bash
# 生成图标和版本信息
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
goversioninfo versioninfo.json

# 编译
go build -v -o lock.exe -trimpath -ldflags "-s -w -linkmode internal -buildid= -X 'main.AppVersion=v0.0.1' -X 'main.GoVersion=`go version`'" .
```
