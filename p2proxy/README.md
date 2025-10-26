## 介绍

位于不同局域网的任意两个节点，通过具有公网IP的中间服务器，使用UDP打洞的方式，建立连接。
后续流量不再通过中间服务器转发，两个P2P节点自行独立通信，可以代理转发彼此的网络流量。

## 使用说明

1. 实现了：Tracker（UDP 注册/撮合），Node（节点逻辑）、简易协议（JSON + base64 数据字段），以及一个非常小的 SOCKS5（仅 CONNECT，无认证）前端。
2. 协议消息类型包含：register / lookup / notify / peer / stream_open / stream_data / stream_close。
3. CLI: 支持两种模式 `tracker` 和 `node`。`node` 支持启动本地 `socks5`（-socks）并通过 `-peer` 指定远端节点 id。

## 运行示例

在 nodeA (或运行 nodeA 的主机) 配置浏览器或 curl 使用 SOCKS5 代理 127.0.0.1:1080，访问任意 HTTP(S) 地址，nodeA 会把连接请求封装并通过 UDP 发到 nodeB，nodeB 对目标发起 TCP 请求并把响应通过 UDP 返回。

```bash
# 公网IP。如：220.181.7.203
./main -mode=tracker -listen=:40000

# 启动节点 B（用于建立到真实目标的代理请求方/远端）
./main -mode=node -id=nodeB -tracker=220.181.7.203:40000

# 启动节点 A（本地运行 SOCKS，所有本地应用通过 SOCKS 访问，流量经 nodeB 代表转发）
# windows设置代理IP地址填：socks5=127.0.0.1
./main -mode=node -id=nodeA -tracker=220.181.7.203:40000 -socks=127.0.0.1:1080 -peer=nodeB
```

## 工作原理

初始注册：节点向 tracker 的 UDP 地址发送 {"type":"register","from":"<id>"}，tracker 保存节点公网/映射地址。
查找/撮合：节点向 tracker 请求 lookup，tracker 会把对端地址返回给请求方并同时通知对端 requester 的地址（以便双方发送 UDP 包进行打洞）。
P2P 数据通道：应用层（SOCKS5）在本地打开 TCP 连接后，生成 stream_open 消息（包含目标 host:port 与 stream_id）发往对端；对端收到 stream_open 后代表发起方连接目标。后续数据用 stream_data（payload base64）和 stream_close 传输。

## TODO

- NAT 穿透：实现中 tracker 会把对端地址同时发给双方以便打洞，但没有实现专门的重复打洞/探测逻辑（可在需要时补充定期发送打洞包直到收到响应）。对称 NAT 下不能保证成功；若需要高可靠性需要 STUN/TURN/更复杂的技术。
- 传输可靠性：目前的数据通道基于 UDP + JSON + base64，属于“尽力而为”（best-effort）。没有实现序号、确认、重传或重排序处理。该原型可工作于大多数简单 NAT/网络，但会丢包/乱序。
- 加密/认证：没有实现任何加密或身份验证，生产环境必须加入加密（例如 DTLS、TLS/QUIC 或在消息层加 AEAD）以及节点权限控制。
- SOCKS5：实现是简化版，仅支持 CONNECT（TCP）。没有实现 UDP ASSOC、用户名认证等。
- 性能：JSON + base64 不适合高性能场景；实际产品应切换为二进制帧（protobuf/msgpack/自定义）并减少拷贝与编码开销。