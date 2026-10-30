# 发行说明

1.0.0 的 `runtime-baseline.json` 只记录现机公开程序和资源的路径、大小、SHA256。372 个文件已与发行包解包内容逐一核对。默认配置使用通用空值，不复制设备 UCI 或订阅。

发行公钥：`keys/egressveil-release.pem`。SHA256：

```
af3aa345ecbddfe44ff7f390c62d2f7cbc981b334c0ffd45b2fe647765d98d57
```

## 构建与验签

使用 OpenWrt 25.12 的 x86/64 源码或匹配 SDK，把仓库 `package/luci-app-egressveil` 放入 `package/`，准备 feeds 和依赖后选包构建：

```sh
make defconfig
make package/luci-app-egressveil/compile V=s
```

固件构建同时选择所需系统依赖；软件包不会自行下载不匹配的内核模块。预构建程序保持现机字节，SDK 二次 strip 已关闭。UA 严格引擎和控制器的构建脚本、Go 依赖，以及 Mihomo 对应版本源码都在仓库内。完整对应源码随 Release 提供。

自己编译后应使用自己的 APK 签名密钥。官方发行私钥不公开，因而自己构建的签名和 APK 哈希会不同；程序基线可用上述清单独立比较。不要使用第三方私钥，也不要在路由器上关闭验签。

```sh
apk verify --keys-dir ./keys ./luci-app-egressveil-1.0.0-r1.apk
```

编译源码包只包含 OpenWrt package 目录和说明；修改 Go 核心时下载完整源码包。Release 的 SHA256SUMS 用于传输校验，APK 签名才是安装验签依据。
