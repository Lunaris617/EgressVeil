//go:build linux
package main
import("os";"syscall";"errors")
func checkConfigOwner(st os.FileInfo) error {s,ok:=st.Sys().(*syscall.Stat_t);if !ok||s.Uid!=0{return errors.New("topology owner is not root")};return nil}
