# 架构与功能删减

EgressVeil 是一个固件软件包和一个 LuCI 入口，不是把代理核心、HTTP 解析器与内核规则硬塞成一个进程。

```text
LuCI EgressVeil → /etc/config/egressveil → 统一配置与事务控制
                 ├─ ua3f procd：双 TPROXY + 严格 HTTP 引擎
                 ├─ OpenClash/Mihomo：fake-ip、DNS、规则及节点/UDP
                 └─ 桥/GRO/TTL：就绪审计、出口规则与故障保护
```

保留内部 `ua3f-integration` 路径、`ua3fi_*` 规则名和已验证的 mark，避免品牌重命名同时改变网络语义。用户只操作 EgressVeil 页面；独立 UA3F/OpenClash 前台和分别升级入口不安装。

## UA3F 交付核心

基于先前定制的 UA3F 3.7.0 严格 HTTP 引擎，协议解析源码保留。新的 Linux 入口只监听 1080/1081，按连接固定使用 `0xc9` / `0x1cb`；维持 GID 65534、防自环和原有 procd 监督。

保留：HTTP/1.0、HTTP/1.1、连接复用、明文 HTTP/2 prior-knowledge/h2c、HPACK/多流、透明非 HTTP/TLS，以及透明流内的 HTTP CONNECT/SOCKS 握手识别。

不交付：原版多模式 CLI、独立 HTTP/SOCKS 服务、REDIRECT/NFQUEUE、独立管理 API、旧白名单/目的缓存引擎、正则和部分替换、自定义正文/URL规则、MitM 证书、L3 TTL/IPID/TCP时间戳/窗口伪装、QUIC 拒绝、BPF offload、Desync 及旧统计前台。TTL 交由整体策略维护，避免双重写入。

源码仅 `core/internal/strict`、透明 socket 传输和精简启动程序。严格引擎识别到 HTTP 后解析失败关闭连接；不能回退成未经改写的明文。每条拨号的 mark 不共享可变状态。日志不记录请求正文、认证材料或目标地址。

## 代理和 DNS

fake-ip TCP 先由 Clash 恢复域名并选择 DIRECT/节点，后续 TCP 进入最终出口处理。UDP 经统一入口交给 Mihomo，按规则 DIRECT 或代理，不全局强制直连。国际 DNS 使用跟随自动代理策略的 DoH；国内/节点启动解析保留 DIRECT 引导。

`Switch` 支持自动选择及手动节点。provider 每小时刷新；探测默认 30 秒，容差 100ms。订阅内的节点证书设置按核心支持读取，不固化某一节点指纹。

OpenClash 的临时配置与最终配置均按精确路径及 root 所有权校验。普通和快速启动都会在核心运行前执行策略规范化；失败禁止启动。健康检查进一步逐项比较实际生成 DNS 与源策略，不只查询进程存活。

## TTL 与生命周期

严格配置固定 IPv4 TTL=64：保留原 TCP/GRO 就绪保护，WAN netdev egress 执行统一 TTL。停用 UA 仍保留 TTL/分流；整体关闭才撤销整体接管。IPv6 不属于此实现的保护范围，有 IPv6 默认路由时拒绝激活。

首次激活使用私有事务备份；失败恢复，成功提交 activated。初始无订阅不接管。已启用核心崩溃时规则保留，procd 重启。正常系统 shutdown 保留用户启用选择，避免把重启当作主动关闭。

桥接采用原有已审查模型：LAN per-bridge=0、IPv4 global=0、已审查 Docker 桥=1；未知结构保留隔离并拒绝就绪。GRO 仅关闭 rx-gro-list，保留普通 GRO。保留对 offload、XDP/TC 等的只读绕过审计，不启用 BPF 伪装。
