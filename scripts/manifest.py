#!/usr/bin/env python3
from pathlib import Path
import hashlib,json
root=Path(__file__).resolve().parents[1]
payload=root/'package/luci-app-egressveil/files'
rows=[]
for p in sorted(payload.rglob('*')):
 if p.is_file():rows.append({'path':p.relative_to(root).as_posix(),'bytes':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()})
(root/'ASSETS.json').write_text(json.dumps({'version':'0.1.0-dev','target':'OpenWrt 25.12 x86/64','files':rows},ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
print('ASSET_MANIFEST_WRITTEN',len(rows))
