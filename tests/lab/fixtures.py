"""Synthetic loopback-only test services. No real subscription or credentials."""
import socket,ssl,threading,select,json,http.server,argparse
class HTTP(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  if self.path=='/subscription':
   body=b'proxies:\n  - name: lab-tls-socks\n    type: socks5\n    server: 10.0.3.2\n    port: 19081\n    tls: true\n    skip-cert-verify: true\n    udp: false\n'
   code=200
  else:
   body=json.dumps({'ua':self.headers.get('User-Agent'),'path':self.path}).encode();code=200
  self.send_response(code);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
 def log_message(self,*a):pass
def exact(c,n):
 b=b''
 while len(b)<n:
  v=c.recv(n-len(b))
  if not v:raise EOFError()
  b+=v
 return b
def relay(c,addr):
 upstream=None
 try:
  c=TLS.wrap_socket(c,server_side=True)
  a=exact(c,2);exact(c,a[1]);c.sendall(b'\x05\x00')
  a=exact(c,4)
  if a[1]!=1:raise ValueError('TCP fixture only')
  if a[3]==1:host=socket.inet_ntoa(exact(c,4))
  elif a[3]==3:host=exact(c,exact(c,1)[0]).decode()
  elif a[3]==4:host=socket.inet_ntop(socket.AF_INET6,exact(c,16))
  else:raise ValueError()
  port=int.from_bytes(exact(c,2),'big')
  # Guest host alias means this lab machine, not the physical LAN.
  if host in ('10.0.3.2','10.0.4.2') or host.endswith('.egressveil-lab.com'):host='127.0.0.1'
  upstream=socket.create_connection((host,port),20)
  c.sendall(b'\x05\x00\x00\x01'+b'\0'*6)
  while True:
   readable,_,_=select.select([c,upstream],[],[],60)
   if not readable:break
   for a in readable:
    data=a.recv(65536)
    if not data:return
    (upstream if a is c else c).sendall(data)
 except Exception:pass
 finally:
  c.close()
  if upstream:upstream.close()
def socks():
 s=socket.socket();s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1);s.bind(('127.0.0.1',19081));s.listen()
 while True:threading.Thread(target=relay,args=s.accept(),daemon=True).start()
def udp():
 s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.bind(('127.0.0.1',19082))
 while True:
  b,a=s.recvfrom(65535);s.sendto(b,a)
if __name__=='__main__':
 parser=argparse.ArgumentParser();parser.add_argument('--cert',required=True);parser.add_argument('--key',required=True);args=parser.parse_args()
 TLS=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);TLS.load_cert_chain(args.cert,args.key)
 threading.Thread(target=socks,daemon=True).start();threading.Thread(target=udp,daemon=True).start()
 print('Synthetic HTTP/TLS-SOCKS/UDP fixtures ready on loopback',flush=True)
 http.server.ThreadingHTTPServer(('127.0.0.1',19080),HTTP).serve_forever()
