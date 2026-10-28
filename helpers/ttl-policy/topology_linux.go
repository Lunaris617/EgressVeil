//go:build linux
package main
import("os";"syscall";"errors";"strings";"ua3f-ttl-policy/internal/guard")
func checkConfigOwner(st os.FileInfo) error {s,ok:=st.Sys().(*syscall.Stat_t);if !ok||s.Uid!=0{return errors.New("topology owner is not root")};return nil}
func setGuardTopology() error {guard.LANInterface=lanBridge;guard.WANInterface=wanDevice;guard.Definition=strings.ReplaceAll(strings.ReplaceAll(guard.Definition,`"br-lan"`,`"`+lanBridge+`"`),`"eth1"`,`"`+wanDevice+`"`);return nil}
