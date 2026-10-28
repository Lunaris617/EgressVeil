# 一次性虚拟机检查

只用于隔离虚拟机。feature-matrix.py 会修改测试机 UCI、启停服务，不能对生产路由器运行。

测试使用两台 OpenWrt 虚拟机，连接在隔离 LAN：路由器为 192.168.77.1，客户端为 192.168.77.2。路由器 SSH 映射到本机 127.0.0.1:22228，客户端到 127.0.0.1:22229。实验目录中放置自己的 id_ed25519 与 known_hosts，不随项目分发。

路由器 QEMU 用户网络中的宿主别名为 10.0.3.2。fixtures.py 仅监听宿主回环，提供合成订阅、HTTP 回声、TLS SOCKS 和 UDP 回声；需要预先创建仅供实验使用的证书：

```sh
umask 077
mkdir -p "$LAB"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -keyout "$LAB/fixture.key" -out "$LAB/fixture.crt" -subj /CN=isolated-lab.invalid
python3 tests/lab/fixtures.py --cert "$LAB/fixture.crt" --key "$LAB/fixture.key"
```

测试节点的 skip-cert-verify 只用于这一自签名合成端点，不是生产订阅建议。

客户端需要配置以 192.168.77.2 为源的策略路由，默认下一跳为 192.168.77.1，避免测试经第二块网卡绕行。先把 `tests/udp_echo_client.go` 编译为 Linux 程序，复制到客户端 `/tmp/udp-test`。

```sh
EGRESSVEIL_DISPOSABLE_LAB=1 EGRESSVEIL_LAB_DIR="$LAB" \
  python3 tests/lab/feature-matrix.py
```

结果写入 tests/results/feature-matrix.json；该目录不提交 Git。脚本检查八种组合的应用结果、HTTP/UA、UDP 回声、fake-IP DNS 和 TTL 规则。真实 WAN 报文验证另用受控探针和 tcpdump，不能只依赖规则检查。

HTTP/h2c 和控制网关单元测试见维护文档。LAN/WAN 抓包、模式切换、异常退出、回滚及重启的本次实测汇总见 docs/validation.md。
