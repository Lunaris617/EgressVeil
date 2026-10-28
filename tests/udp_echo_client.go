package main

import("flag";"fmt";"net";"os";"bytes";"time")
func main(){
 bind:=flag.String("bind","127.0.0.1:0","local IPv4 address:port")
 target:=flag.String("target","127.0.0.1:19082","UDP echo target")
 flag.Parse()
 l,e:=net.ResolveUDPAddr("udp4",*bind);if e!=nil{panic(e)}
 r,e:=net.ResolveUDPAddr("udp4",*target);if e!=nil{panic(e)}
 c,e:=net.DialUDP("udp4",l,r);if e!=nil{panic(e)};defer c.Close()
 for _,size:=range []int{16,256,1200}{
  msg:=bytes.Repeat([]byte("x"),size);c.SetDeadline(time.Now().Add(4*time.Second))
  if _,e=c.Write(msg);e!=nil{fmt.Println("UDP_WRITE_FAILED");os.Exit(1)}
  buf:=make([]byte,2048);n,e:=c.Read(buf)
  if e!=nil||!bytes.Equal(buf[:n],msg){fmt.Println("UDP_ECHO_FAILED",size);os.Exit(1)}
  fmt.Println("UDP_ECHO_OK",size)
 }
}
