# 构建、维护与回滚

普通固件构建复制 `package/luci-app-egressveil`，选入一个包即可。升级时整体替换旧 package 目录，不混入旧版本文件。依赖的内核模块必须与目标固件同树编译；不能用旧固件的 kmod 替代新内核模块。

## 源码维护

已固定 UA3F 定制核心、OpenClash 0.48.002、Mihomo 1.19.32 与 Metacubexd 1.273.1。Mihomo 对应源码位于 `sources/`；UA3F、控制网关及辅助程序源码和 Go vendor 随仓库提供。上游许可证见 NOTICE / LICENSES。

使用 Go 1.25.3：

```sh
sh scripts/test-core.sh
(cd helpers/controller && go test ./...)
(cd helpers/ttl-policy && go test -mod=vendor ./...)
sh scripts/build-core.sh
sh scripts/build-helpers.sh
python3 scripts/manifest.py
python3 scripts/check-distribution.py
```

HTTP 协议测试不需要 root。Linux SO_MARK 实测需要 root/CAP_NET_ADMIN，应在隔离环境执行。TTL helper 的 ethtool 依赖为固定 hash；替换该程序必须同步期望值并重新验收。

打包已禁用 SDK 二次 strip，确保预构建程序与 ASSETS.json、运行时信任哈希一致。

仪表盘静态文件已随包提供，固件编译时不再下载。更新组件后运行相应回归，统一发布完整包，不使用独立 OpenClash/核心安装器覆盖整合版本。

## 设备维护

优先使用 LuCI 保存与应用。可以查看状态和执行预检查：

```sh
/usr/libexec/egressveil/setup status
/usr/libexec/egressveil/setup preflight
cat /var/run/egressveil/result
logread -e egressveil
```

最近应用结果与当前就绪状态分别显示。保存应用后刷新页面；“接管已就绪”表示本地核心及接管条件通过，不代替外网连通性或节点可用性检查。原概览和仪表盘提供节点探测与访问检查。

```sh
# 应用已保存的配置
/etc/init.d/egressveil reload
# 整体暂停，保留三个功能选择
/etc/init.d/egressveil stop
# 按原选择恢复；纯直连不要求订阅
/etc/init.d/egressveil start
```

应用中断留下事务时，使用 `setup recover` 恢复后核对状态。运行中的应用锁不会被 recover 强行移除。不要手动删除保护规则后继续运行已启用的 UA 服务。

`/etc/egressveil/`、OpenClash 配置/提供商目录和 UCI 属于设备私有状态，软件包声明保留。发布目录只含空订阅默认配置，不包含运行日志或私钥。移植到新固件时先保留整盘备份与独立管理通道；当前实体硬件仍需单独验收。

## 隔离验证

`tests/lab/feature-matrix.py` 提供八种组合的检查。必须显式确认一次性虚拟机环境，并提供实验目录及合成订阅；测试脚本会修改测试机配置，不能对生产路由器运行。协议与控制接口单元测试不需要该环境。

`tests/ttl_probe_linux.go` 是受控 UDP 回声探针，可设置 TTL，供 WAN 头部抓取验证。不要用“规则存在”代替出站包验证，也不要用 STUN 成功代替真实跨网 P2P 验收。
