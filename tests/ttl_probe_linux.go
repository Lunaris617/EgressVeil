//go:build linux

// Sends synthetic UDP data with a chosen IPv4 TTL for egress verification.
package main
import("flag";"net";"syscall";"fmt";"os";"time")
func main(){
 target:=flag.String("target","127.0.0.1:19082","controlled UDP echo target")
 ttl:=flag.Int("ttl",128,"IPv4 TTL (1..255)");flag.Parse()
 if *ttl<1 || *ttl>255{os.Exit(2)}
 a,e:=net.ResolveUDPAddr("udp4",*target);if e!=nil{os.Exit(1)}
 c,e:=net.DialUDP("udp4",nil,a);if e!=nil{os.Exit(1)};defer c.Close()
 raw,e:=c.SyscallConn();if e!=nil{os.Exit(1)}
 var optionErr error
 if raw.Control(func(fd uintptr){optionErr=syscall.SetsockoptInt(int(fd),syscall.IPPROTO_IP,syscall.IP_TTL,*ttl)})!=nil || optionErr!=nil{os.Exit(1)}
 c.SetDeadline(time.Now().Add(3*time.Second));msg:=[]byte("EGRESSVEIL-SYNTHETIC-TTL-PROBE")
 if _,e=c.Write(msg);e!=nil{os.Exit(1)}
 b:=make([]byte,128);n,e:=c.Read(b);if e!=nil || string(b[:n])!=string(msg){os.Exit(1)}
 fmt.Println("TTL_PROBE_ECHO_OK")
}
