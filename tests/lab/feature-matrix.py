"""Feature matrix on the loopback-only disposable VMs; no production targets."""
from pathlib import Path
import subprocess,json,time,itertools,sys,os
if os.environ.get('EGRESSVEIL_DISPOSABLE_LAB')!='1':
 raise SystemExit('Set EGRESSVEIL_DISPOSABLE_LAB=1 only for disposable test VMs')
L=Path(os.environ['EGRESSVEIL_LAB_DIR']).resolve()
REPORT=Path(__file__).resolve().parents[2]/'tests/results/feature-matrix.json'
REPORT.parent.mkdir(parents=True,exist_ok=True)
def ssh(i,cmd,timeout=180):
 r=subprocess.run(['ssh','-i',str(L/'id_ed25519'),'-o','StrictHostKeyChecking=no','-o','UserKnownHostsFile='+str(L/'known_hosts'),'-o','LogLevel=ERROR','-p',str(22228+i),'root@127.0.0.1',cmd],capture_output=True,timeout=timeout)
 return r.returncode,r.stdout.decode(errors='replace'),r.stderr.decode(errors='replace')
results=[]
for proxy,ua,ttl in itertools.product((0,1),repeat=3):
 ident=f'{proxy}{ua}{ttl}'
 cmd=f'''uci set egressveil.main.enabled=1
uci set egressveil.main.configured=1
uci set egressveil.main.proxy_enabled={proxy}
uci set egressveil.main.ua_enabled={ua}
uci set egressveil.main.ttl_enabled={ttl}
uci set egressveil.main.proxy_mode=rule
uci set egressveil.main.ttl=64
uci set egressveil.main.feed=http://10.0.3.2:19080/subscription
uci commit egressveil
/usr/libexec/egressveil/setup apply >/var/run/egressveil/matrix-apply.log 2>&1
code=$?
cat /var/run/egressveil/result
exit "$code"
'''
 rc,out,err=ssh(0,cmd)
 row={'combination':ident,'apply':rc==0 and 'READY' in out,'result':out.strip()}
 if row['apply']:
  # Synthetic request and echo data only.
  a,b,c=ssh(1,f"curl -fsS --noproxy '*' --interface br-lan --max-time 15 http://10.0.3.2:19080/matrix-{ident} -A ORIGINAL-UA")
  try: row['http']=a==0;row['ua']=json.loads(b)['ua'];row['ua_expected']=row['ua']==('FFF' if ua else 'ORIGINAL-UA')
  except Exception:row['http']=False;row['http_error']=c[-200:]
  a,b,c=ssh(1,'/tmp/udp-test -bind 192.168.77.2:0 -target 10.0.3.2:19082');row['udp']=a==0;row['udp_sizes']=b.strip().splitlines()
  a,b,c=ssh(1,'nslookup example.com 192.168.77.1');row['dns']=a==0 and '198.18.' in b
  a,b,c=ssh(0,'nft list chain netdev ua3fi_ttl_last egress 2>/dev/null');row['ttl_rule']=(a==0 and 'ip ttl set 64' in b) if ttl else a!=0
 else:
  a,b,c=ssh(0,"tail -n 12 /var/run/egressveil/matrix-apply.log | sed '/authentication/d'");row['failure']=b[-1600:]
 results.append(row);print(json.dumps(row),flush=True)
 REPORT.write_text(json.dumps(results,indent=2),encoding='utf-8')
 if not row['apply']:break
sys.exit(0 if len(results)==8 and all(x.get('http') and x.get('ua_expected') and x.get('udp') and x.get('dns') and x.get('ttl_rule') for x in results) else 1)
