package main
import("os";"regexp";"strings";"errors")
var lanBridge="br-lan"
var lanPort="eth0"
var wanDevice="eth1"
func loadRuntimeTopology() error {
 const path="/etc/ua3f-integration.conf"
 st,e:=os.Lstat(path);if e!=nil{return e}
 if st.Mode()&os.ModeSymlink!=0 || !st.Mode().IsRegular() || st.Mode().Perm()&0022!=0{return errors.New("unsafe topology file")}
 if e=checkConfigOwner(st);e!=nil{return e}
 b,e:=os.ReadFile(path);if e!=nil{return e};m:=map[string]string{}
 for _,l:=range strings.Split(string(b),"\n"){p:=strings.SplitN(strings.TrimSpace(l),"=",2);if len(p)==2{m[p[0]]=p[1]}}
 valid:=regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)
 for _,k:=range []string{"LAN_IFACE","LAN_PORT","WAN_IFACE"}{if !valid.MatchString(m[k])||m[k]=="lo"{return errors.New("invalid suite topology")}}
 if m["LAN_IFACE"]==m["WAN_IFACE"]||m["LAN_PORT"]==m["WAN_IFACE"]{return errors.New("overlapping suite topology")}
 lanBridge=m["LAN_IFACE"];lanPort=m["LAN_PORT"];wanDevice=m["WAN_IFACE"]
 return nil
}
