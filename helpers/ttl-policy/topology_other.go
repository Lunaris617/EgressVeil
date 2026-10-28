//go:build !linux
package main
import "os"
func checkConfigOwner(st os.FileInfo) error{return nil}
func setGuardTopology() error{return nil}
