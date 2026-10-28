#!/usr/bin/env python3
"""Check public distribution boundaries; never read device state."""
from pathlib import Path
import hashlib,re,json
root=Path(__file__).resolve().parents[1]
payload=root/'package/luci-app-egressveil/files'
cfg=(payload/'etc/config/egressveil').read_text(encoding='utf-8')
assert "option feed ''" in cfg and "option enabled '0'" in cfg
for p in payload.rglob('*'):
 if not p.is_file():continue
 rel=p.relative_to(payload).as_posix()
 assert not rel.startswith(('root/','etc/dropbear/','etc/shadow','etc/passwd','var/'))
 assert p.suffix not in ('.key','.pem','.crt','.p12','.log','.pcap','.pcapng','.jsonl','.bak')
 if p.suffix in ('.dat','.mmdb'):continue
 data=p.read_bytes()
 assert not re.search(rb'-----BEGIN ([A-Z ]*PRIVATE KEY)-----\s+[A-Za-z0-9+/=]{40,}.*?-----END \1-----',data,re.S),rel
for path,expected in {
 'package/luci-app-egressveil/files/etc/openclash/core/clash_meta':'7a0d59da2e678d56c899a3db996a2ad8963286c4f3634b0451435db248f13fa1',
 'sources/mihomo-v1.19.32.tar.gz':'ab130b7fab3893d01aa44c0d39be34a26da716c1d36832fbac9fa054a1d862a0',
 'package/luci-app-egressveil/files/usr/libexec/ua3f-integration/ethtool-feature':'f65ba4069f40542d00a9c942cb8bbe85d1a8cc9e0baa18fce93c23e8215e0895',
}.items():assert hashlib.sha256((root/path).read_bytes()).hexdigest()==expected,path
print(json.dumps({'empty_subscription':True,'no_device_state_in_payload':True,'frozen_upstream_assets_verified':True}))
