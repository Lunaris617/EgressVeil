# dev 验证记录

日期：2026-10-28。现网路由器没有安装本包或修改服务。实验在本机 WSL/KVM 两台隔离 OpenWrt 25.12.6 / Linux 6.12.112、virtio_net 中进行，provider 为合成测试订阅，不使用真实节点凭据。

| 项目 | 结果 |
|---|---|
| 精简协议核心 | 25 个顶层协议测试通过，包含 HTTP/1、h2c/HPACK、复用及隧道等；新核心约 4MB |
| Linux socket | VM root 下实际 40 条并发拨号的 0xc9/0x1cb mark 均正确；本地监听防环测试通过 |
| 单 APK | 官方 25.12.6 SDK 生成 luci-app-egressveil-0.1.0-r1.apk；不是四个独立插件改名 |
| 无订阅 | 未启动 UA3F/Mihomo、未接管，普通 LAN HTTP 可用 |
| 首次激活 | READY；rule 模式；provider 健康检查通过 |
| 手动/自动选择 | API 确认 Switch 从自动切到合成节点，再切回自动，配置生效 |
| 明文 HTTP | LAN 真实地址及路由器本机请求，接收端看到 FFF |
| fake-ip DIRECT | 临时测试规则指定公共域名 DIRECT，DNS 给 198.18 地址，HTTP 200，WAN 明文 UA=FFF |
| fake-ip 代理 | 另一公共域名取得 198.18 地址并返回 HTTP 200；加密节点内层 UA 不冒充 WAN 改写证据 |
| UDP | 16/256/1200 字节均双向内容一致；测试为 DIRECT，未据此声称跨网 P2P 通过 |
| ICMP | 显式绑定 LAN 源地址，2/2 回包 |
| WAN TTL | 出站抓到 70 个 IPv4 样本，均 TTL64；抓包内核丢包 0 |
| UA 配置 | 将统一值改为 EgressVeil-Check 后接收端看到该值；再恢复 FFF 成功 |
| UA 单独关闭 | 进程停止，接收端看到原测试 UA；TTL64 规则及分流保留；重新启用恢复 FFF |
| 意外退出 | SIGKILL 后独立保护表保留；procd 恢复新进程并就绪 |
| 防火墙重载 | 重新就绪且 HTTP FFF 正常 |
| 整机重启 | 整体 enabled/activated 保留；后端健康、rule 模式、HTTP FFF 恢复 |
| LuCI | HTTP 200，新的 EgressVeil 表单和 TTL 项实际渲染，独立 UA3F/OpenClash controller 未安装 |
| 应用锁 | 不允许 recover 干扰仍在运行的应用；退出进程遗留锁可回收并重新 READY |
| 最终 DNS 配置 | 处理 OpenClash 的实际临时文件路径；普通/快速启动在核心启动前强制规范化，生成 DNS 与源策略逐项一致才通过健康检查；独立 dashboard 已禁用 |

不是长期或任意硬件验收：未覆盖真实 r8169 新内核的长测、多桥/VLAN/PPPoE、IPv6、Docker/打印、真实 HY2/QUIC 全协议矩阵和独立跨网 P2P。LuCI 表单渲染、后端 API 和配置生效已测，未自动化完整浏览器点击流程。刷入实际硬件后需做短验收，`dev` 不能当作稳定版。

实验镜像采用与 package 相同的 files 载荷及依赖生成；SDK APK 独立编译验证。没有在物理生产设备上安装 APK。临时域名规则、测试密钥、订阅和抓包只在实验目录，不进入源码默认配置。

启动过渡期会在 OpenClash 链尚未建立时记录等待/缺链提示，保护路径保持；就绪后检查通过。另观察到上游辅助脚本的 `out of range` 提示，没有阻止此次功能验收，其兼容性整理仍属于 dev 后续工作，不宣称日志零告警。
