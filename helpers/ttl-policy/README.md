# TTL/GRO readiness policy, portable suite revision

The frozen native TCP gate remains fail-closed until topology, offload and GRO
checks pass. It reads root-owned /etc/ua3f-integration.conf rather than using
the old deployment snapshot. Supported physical names are eth0/eth1, one LAN
bridge port and a separate WAN. LAN driver: r8169 or lab virtio_net.

Only rx-gro-list is requested off; ordinary GRO remains on. The unchanged
ethtool helper must have SHA256
f65ba4069f40542d00a9c942cb8bbe85d1a8cc9e0baa18fce93c23e8215e0895.
The helper source is in ../ethtool-feature. The suite additionally installs an
owned netdev egress IPv4 TTL=64 rule; this program alone is a readiness gate,
not that TTL rewrite implementation.

Commands: barrier, apply, check, remove. Root required for mutations. check is
read-only. Tests cover topology, forbidden offload, feature state and nft gate
shape; they do not simulate a physical NIC. See top-level VALIDATION.md.

Use scripts/rebuild-helpers.sh with Go 1.25.3. Ordinary firmware compilation
uses prebuilt binaries and does not need Go modules. Maintainer rebuilds need
the dependencies in the module cache or go mod download first.
