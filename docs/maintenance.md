# 构建、维护与回滚

普通固件构建只复制 `package/luci-app-egressveil`，选择该单一包；不需要本机 Go，也不在固件构建时下载三个核心的“最新版”。依赖内核模块必须与新固件同树编译；本包不会解决任意自定义内核和官方 kmod 源的 ABI 不匹配。

内核保持标准 IPv6 编译支持（部分 fw4 模块需要），在网络配置层关闭可用 IPv6 路由。本版仅验收 IPv4。软件包默认普通 LAN/WAN 逻辑名，可在页面调整；物理双口及驱动限制见 README。

## 维护精简核心

在 Linux、Go 1.25.3 中：

```sh
cd core
go test -mod=vendor -count=1 ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 \
  go build -mod=vendor -trimpath -buildvcs=false \
  -ldflags='-s -w -buildid=' \
  -o ../package/luci-app-egressveil/files/usr/bin/ua3f ./cmd/ua3f
```

`internal/transport` 的真实 SO_MARK 测试需要 Linux root/CAP_NET_ADMIN，普通用户运行时该项会跳过；协议测试不需要 root。宿主机权限只用于测试 socket，不修改宿主机防火墙。生产生命周期测试应在隔离固件里进行。

`helpers/ttl-policy`、`helpers/bridge-audit` 的构建规则与原有辅助源码保留；Go 依赖已 vendor。ethtool helper 是固定 hash 的已审查程序：重新构建后若 hash 改变，必须同步修改 TTL helper 的期望值并重新验收，不能简单覆盖文件。

Mihomo 保留 v1.19.32 with_gvisor 固定二进制及 `sources/` 对应源码。OpenClash 基于固定提交的 MIT 引擎文件，安装自带版权说明；不安装其单独 UI。升级任一组件后需重新回归，不通过其上游安装器分别覆盖整合文件。

## 设备侧维护

```sh
/usr/libexec/egressveil/setup status
/usr/libexec/egressveil/setup preflight
cat /var/run/egressveil/result
logread -e egressveil
```

修改配置优先使用 LuCI 页面保存应用。CLI 显式重载：

试验版保存应用会重新加载内部服务，已有连接可能短暂重连；当前不是仅修改单一节点的无损热切换实现。

```sh
uci commit egressveil
/etc/init.d/egressveil reload
```

紧急整体停用：

```sh
/etc/init.d/egressveil stop
```

这会保存整体关闭，撤销本套件接管并停止相关后端。再次启动需已有有效订阅：`/etc/init.d/egressveil start`。系统重启本身不会取消启用。

若激活中断并留下事务，可执行 `setup recover` 恢复，再检查状态。首次激活中断后的正常启动也会恢复。先保留整盘备份和独立管理通道；首版不覆盖升级现网的旧套件，需要在新固件使用。

`/etc/egressveil/`、订阅生成 YAML、UCI、provider 缓存和日志都属于设备私有状态，禁止提交 Git。源码仓库只包含空订阅默认配置与公开程序/数据。GPLv3 及原作者许可适用于再分发，NOTICE/LICENSES 必须随发布保留。
