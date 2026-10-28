# EgressVeil

OpenWrt WAN egress normalization: a slim UA3F strict HTTP engine, OpenClash/Mihomo policy routing, and IPv4 TTL control behind one LuCI page and one firmware package.

**GPLv3 · Experimental `dev` · OpenWrt 25.12 x86/64**

EgressVeil 将明文 HTTP UA、代理分流与 IPv4 TTL 整合为一个软件包。它不是“上游无法识别路由器”的承诺。项目自有代码及整体集成采用 GPLv3；原作者版权与第三方许可完整保留，见 [NOTICE](NOTICE) 和 [LICENSES](LICENSES)。

## 编译进固件

1. 准备 OpenWrt 25.12 的 x86/64 源码，更新并安装 feeds。
2. 将本仓库 **`package/luci-app-egressveil`** 整个目录复制到源码的 `package/`。不要再选单独的 UA3F/OpenClash 或旧套件，同名文件不能由两个包管理。
3. `make menuconfig`：选择 **x86 → x86_64**，在 **LuCI → Applications** 把 **`luci-app-egressveil` 选为 `<*>`**。使用 `dnsmasq-full`，取消普通 `dnsmasq`；依赖内核模块会随该选择拉入。
4. 在 **Target Images → Root filesystem partition size** 将根分区设为 **512MiB 或更大**；运行 `make defconfig`，然后执行正常的 `make download -j8` 和固件编译。

固件包只有一个 APK；运行时保留独立的 procd 子实例，避免重写已经验证的规则与监督逻辑。仓库预置 x86_64 用户态程序和公开路由数据，因此编译本包不用临时下载核心。**完整 OpenWrt 的工具链和其他系统依赖仍需先下载**。

## 刷机后

先配置自己的 LAN/WAN、DHCP、SSH 与校园网认证。进入 **服务 → EgressVeil**，填入订阅、启用并保存应用，稍后刷新确认 `READY`。不需要另外配置 UA3F 或 OpenClash 页面。

- 默认规则模式：国内直连，其余由“自动选择”或手选节点处理。
- UDP/QUIC 同样按规则分流；代理节点须支持所需 UDP。没有独立对端不能保证所有 P2P/NAT 场景。
- HTTP/1、明文 HTTP/2/h2c 严格改写 UA，默认 `FFF`；TLS 透明传输，不安装 MitM 证书。
- WAN 出站 IPv4 TTL 固定 `64`，含 GRO 防绕过就绪检查。首版严格配置不开放其他 TTL 值。
- 可单独关闭 UA 改写并保留 TTL 和 Clash；关闭整个 EgressVeil 才撤销整套接管。
- 自动探测默认 30 秒，可选 15–300 秒。第一次激活后刷新页面，即可看到订阅中的手选节点。

没有订阅时不接管；首次激活失败恢复之前配置；已启用核心意外退出保留保护并由 procd 拉起。重启按保存的状态恢复。当前是实验版，刷入实际硬件后仍要做短验收，不直接宣称适用于任何新内核和网络拓扑。

## 当前边界

面向独立 WAN＋一个 LAN 物理口的 x86/64，物理口为 `eth0`/`eth1`，LAN 驱动为 `r8169`（`virtio_net` 用于隔离实验）。逻辑接口名可设置。多 LAN 端口、VLAN/PPPoE、其他网卡/桥或 IPv6 默认路由会拒绝激活，需要另外适配。

不附带校园网工具、账号、系统密码、订阅、节点密钥、真实客户端清单、租约或业务日志。旧设备的个人例外规则不复制。订阅和新生成控制密钥只在用户激活后的设备上保存；不得把其 `/etc` 目录重新提交到仓库。

OpenClash 的独立 UI/部分更新入口不交付，避免更新其中一个组件破坏整合。Docker 不随包安装；若另装 Docker，须先停用套件并复核其版本、启动包装和桥模型，不能默认兼容任意 Docker 网络。

## 源码与验证

- [架构与删减范围](docs/architecture.md)
- [构建、维护与回滚](docs/maintenance.md)
- [实验验证记录](docs/validation.md)
- `core/`：精简核心及严格 HTTP 协议回归测试。
- `helpers/`：TTL/GRO、桥接审计和 ethtool helper 的源码。
- `sources/`：固定 Mihomo 对应公开源码。

`dev` 用于试验；通过真实硬件和长期运行检查后再合并 `main`。不自动更改现网设备，不自动发布稳定固件。
