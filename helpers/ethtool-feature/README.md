# Frozen ethtool helper

This is the source of the unchanged approved feature helper. Build with Go
1.25.3, CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1:

    go build -mod=readonly -trimpath -ldflags="-s -w" -o ethtool-feature ./ethtool-helper

Only eth0/eth1 may receive rx-gro-list SET requests. GET also permits br-lan.
The suite preflight restricts physical port names accordingly. No other feature
is requested for mutation. Run --self-test without privileges; feature changes
require root/CAP_NET_ADMIN. Source dependencies are pinned in go.mod/go.sum.
The ttl-policy binary verifies the frozen helper SHA256 before executing it.
Rebuilding the helper requires revalidation and updating that expected hash.
