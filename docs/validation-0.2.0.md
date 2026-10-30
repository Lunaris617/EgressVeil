# 0.2.0-dev 验证记录

日期：2026-10-29。测试环境为本机 WSL/KVM 内两台隔离 OpenWrt 25.12.6 / Linux 6.12.112，virtio_net。订阅、节点、HTTP、UDP 与 DNS 使用合成测试端，不含生产凭据。生产路由器未安装、未修改。

## 功能组合

0 表示关闭，1 表示开启。每项包含实际 HTTP 回声、UDP 16/256/1200 字节回包、fake-IP DNS 与规则状态检查。

| 代理 | UA | TTL | 接收端 UA | HTTP | UDP / DNS | TTL 规则状态 |
|---|---|---|---|---|---|---|
| 0 | 0 | 0 | ORIGINAL-UA | 通过 | 通过 | 通过 |
| 0 | 0 | 1 | ORIGINAL-UA | 通过 | 通过 | 通过 |
| 0 | 1 | 0 | FFF | 通过 | 通过 | 通过 |
| 0 | 1 | 1 | FFF | 通过 | 通过 | 通过 |
| 1 | 0 | 0 | ORIGINAL-UA | 通过 | 通过 | 通过 |
| 1 | 0 | 1 | ORIGINAL-UA | 通过 | 通过 | 通过 |
| 1 | 1 | 0 | FFF | 通过 | 通过 | 通过 |
| 1 | 1 | 1 | FFF | 通过 | 通过 | 通过 |

## 出站报文

| 场景 | WAN IPv4 样本 | TTL 分布 | 抓包内核丢包 |
|---|---:|---|---:|
| 开启，值 64 | 23 | 64 | 0 |
| 开启，值 128 | 23 | 128 | 0 |
| 关闭 | 23 | 64, 128 | 0 |

受控请求覆盖 fake-IP DIRECT 与 TLS 代理两条 TCP 路径、UDP 和本机 ICMP。DIRECT 接收端 UA 为 FFF；代理请求内层 UA 为原值，但 WAN 上是加密节点连接，不作为“已改写内层 UA”的证据。TTL 关闭后同时存在用户态新建连接的自然 TTL64 和受控原始 TTL128，确认没有残留统一写入。

LAN ICMP 用明确源地址重新验证，2 发 2 收、丢包 0。早期仅绑定接口的探针未命中测试机源策略路由，未将该结果当作插件故障或通过证据。

## 协议、界面与生命周期

- UA 严格引擎 25 个顶层测试通过，覆盖 HTTP/1、h2c/HPACK、多流、复用和已有隧道处理。
- 控制网关边界测试通过：认证、同源、路径归一化、模式参数、日志级别、节点选择及升级/任意路径重载限制。
- 实际 LuCI 概览、设置、订阅、配置页可渲染；原设置页面应用后，设备侧对应值已确认。
- 新卡片界面使用 4 个滑块，保留原 CBI 字段；430px 视口下主布局无横向溢出，主题背景冲突已消除。实际关闭/开启 UA 后，设备侧开关落盘，TTL 与代理保持；登录后的新界面无脚本错误。
- 首次配置三项均未选择，必须明确选择。通过新界面选择三项关闭、订阅留空、启用运行后得到 READY / CORE_READY，Mihomo DIRECT 正常。
- Metacubexd 1.273.1 静态文件随包提供；实际连接、统计和 WebSocket 正常。界面截图已遮蔽地址。
- 旧 EgressVeil 0.1 配置迁移、自动探测间隔修改、手动刷新快速订阅通过。
- 手选节点在 DIRECT / rule 来回切换后保留；切换恢复原策略组和源文件。

以下 13 项生命周期检查均通过：

- controller-mode-direct：通过。
- controller-mode-global：通过。
- controller-mode-rule：通过。
- custom-selector：通过。
- invalid-ttl-rollback：通过。
- custom-ua-source-preserved：通过。
- crash-guard-retained：通过。
- crash-respawn：通过。
- reload-firewall：通过。
- reload-openclash：通过。
- pause-preserves-choices：通过。
- resume：通过。
- reboot：通过。

## 构建与边界

实际单 APK 已通过官方 SDK 构建，离线解包确认包含中文翻译。发布检查已刷新 ASSETS.json：实际 APK 中 374 个运行文件逐一匹配，无哈希差异；默认配置和交付内容扫描通过。SDK 的二次 strip 已关闭，冻结程序保持原字节。截图来自实际隔离界面，不是效果图。

仍待验收：实体 r8169 新固件长测、更多网卡/桥/VLAN/PPPoE、独立跨网 P2P 和真实 HY2/QUIC 完整矩阵。没有测试 IPv6。未做商业生产认证或完整安全审计；不把统一 UA/TTL 表述为无法识别路由器。

服务过渡期可能出现等待链建立的提示；仍观察到上游脚本的 out of range 日志，本次未阻止功能通过。不宣称所有日志零告警。

实际 APK SHA256：`e52c7a28f7fffec4bff05c45e8e3b27e2295e4327a274df6386716a5cfc96d9f`。
