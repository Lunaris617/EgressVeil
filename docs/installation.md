# 安装与升级

## 适用设备

首版为 OpenWrt 25.12 / x86_64 / firewall4 / IPv4。独立以太网 WAN，LAN 网桥仅有一个物理成员；当前支持的物理网口组合为 eth0 / eth1，LAN 驱动 r8169，隔离测试使用 virtio_net。

安装前核对可用内核模块，尤其是 TPROXY、bridge-netfilter、nft netdev egress。只有软件包名称存在还不够：缺少内核钩子的固件需要用匹配源码重编。本包不会强制启用未经检查的拓扑，也不会用其他内核的 kmod 冒充兼容。

## 安装 APK

从同一个 Release 下载 APK、`egressveil-release.pem` 和 `SHA256SUMS.txt`。核对文件 SHA256 后，安装公钥和软件包。

```sh
sha256sum -c SHA256SUMS.txt
mkdir -p /etc/apk/keys
cp egressveil-release.pem /etc/apk/keys/egressveil-release.pem
chmod 0644 /etc/apk/keys/egressveil-release.pem
apk add ./luci-app-egressveil-1.0.0-r1.apk
```

不要使用 `--force-overwrite`、跳过依赖检查或强装不匹配的内核模块。公钥可与仓库 `release/keys/` 下的副本核对。签名私钥不随发行包提供。

APK 自带 UA 严格引擎、Mihomo、LuCI 页面、Metacubexd 和公开规则资源。系统依赖由软件源提供；没有网络的软件源环境需要提前准备匹配的依赖包，不能把这个 APK 当作整套离线固件。

## 从独立插件或开发版本迁移

先保存当前配置和完整磁盘备份，并留一个不依赖被改路由的管理通道。正在使用独立 UA3F、OpenClash 或旧整合包时，处理完文件冲突后再安装，不要直接强制覆盖。旧 init、cron 和防火墙钩子也要检查，避免两个服务同时接管。

已有 EgressVeil 开发版本的用户可用 `apk add --upgrade` 更新。同名配置文件和设备私有状态会保留；首次三项选择不会被重置。不要把别的设备的 `/etc/egressveil/` 或订阅缓存复制过来。

应用配置可能重建连接。首次启用和升级后至少检查：LAN 管理可达、国内/代理出口符合预期、明文 UA、WAN TTL，以及常用 UDP 业务。遇到失败先保留日志和现场，不要先删除保护规则。

## 界面操作

「服务 → EgressVeil → 配置引导 / UA / TTL」里分别选择功能。没有代理需求时可以不填订阅，选择全部直连；Mihomo 仍需运行以完成地址还原和统一接管。

逻辑接口填写 OpenWrt「网络 → 接口」中的名称，通常是 `lan`、`wan`。它们不是物理网口编号。保存应用后，就绪状态在后台检查；深度检查期间表单仍可使用。

节点和策略组从概览中的 Metacubexd 选择。OpenClash 概览的运行总开关会控制整套接管；只想不用代理时，应关闭配置页中的「代理」，不要单独杀掉 Mihomo。

原理、暂停和故障处理见 [architecture.md](architecture.md) 与 [maintenance.md](maintenance.md)。
