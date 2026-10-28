//go:build linux && amd64

// Default: read-only ethtool metadata. Optional SET requests only rx-gro-list.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

type row map[string]any
type feature struct {
	Name         string `json:"name"`
	Active       bool   `json:"active"`
	Fixed        bool   `json:"fixed"`
	Available    bool   `json:"available"`
	Requested    bool   `json:"requested"`
	NeverChanged bool   `json:"never_changed"`
}
type state struct {
	Interface    string    `json:"interface"`
	Driver       string    `json:"driver,omitempty"`
	DriverError  string    `json:"driver_error,omitempty"`
	Features     []feature `json:"features"`
	FeatureError string    `json:"feature_error,omitempty"`
}
type ifreq struct {
	Name [16]byte
	Data uintptr
	Pad  [16]byte
}

var _ [40 - int(unsafe.Sizeof(ifreq{}))]byte
var _ [int(unsafe.Sizeof(ifreq{})) - 40]byte

var order = binary.LittleEndian

func ioctl(fd int, name string, buf []byte) (uintptr, error) {
	if len(name) == 0 || len(name) >= 16 || len(buf) < 4 {
		return 0, errors.New("invalid ioctl arguments")
	}
	req := ifreq{Data: uintptr(unsafe.Pointer(&buf[0]))}
	copy(req.Name[:], name)
	ret, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), unix.SIOCETHTOOL, uintptr(unsafe.Pointer(&req)))
	runtime.KeepAlive(buf)
	runtime.KeepAlive(&req)
	if errno != 0 {
		return ret, errno
	}
	return ret, nil
}
func get(fd int, name string) state {
	s := state{Interface: name, Features: []feature{}}
	drv := make([]byte, 196)
	order.PutUint32(drv, unix.ETHTOOL_GDRVINFO)
	if _, e := ioctl(fd, name, drv); e != nil {
		s.DriverError = e.Error()
	} else {
		s.Driver = strings.TrimRight(string(drv[4:36]), "\x00")
	}
	// struct ethtool_sset_info followed by one count, ETH_SS_FEATURES=4.
	si := make([]byte, 24)
	order.PutUint32(si, unix.ETHTOOL_GSSET_INFO)
	order.PutUint64(si[8:], 1<<4)
	if _, e := ioctl(fd, name, si); e != nil {
		s.FeatureError = "GSSET_INFO: " + e.Error()
		return s
	}
	if order.Uint64(si[8:])&(1<<4) == 0 {
		s.FeatureError = "feature string set unavailable"
		return s
	}
	count := int(order.Uint32(si[16:]))
	if count < 1 || count > 4096 {
		s.FeatureError = "feature count outside safety bound"
		return s
	}
	names := make([]byte, 12+count*32)
	order.PutUint32(names, unix.ETHTOOL_GSTRINGS)
	order.PutUint32(names[4:], 4)
	order.PutUint32(names[8:], uint32(count))
	if _, e := ioctl(fd, name, names); e != nil {
		s.FeatureError = "GSTRINGS: " + e.Error()
		return s
	}
	blocks := (count + 31) / 32
	bits := make([]byte, 8+blocks*16)
	order.PutUint32(bits, unix.ETHTOOL_GFEATURES)
	order.PutUint32(bits[4:], uint32(blocks))
	if _, e := ioctl(fd, name, bits); e != nil {
		s.FeatureError = "GFEATURES: " + e.Error()
		return s
	}
	if int(order.Uint32(bits[4:])) > blocks {
		s.FeatureError = "feature block count grew during query"
		return s
	}
	for i := 0; i < count; i++ {
		off := 8 + (i/32)*16
		mask := uint32(1) << uint(i%32)
		available := order.Uint32(bits[off:])&mask != 0
		never := order.Uint32(bits[off+12:])&mask != 0
		s.Features = append(s.Features, feature{Name: strings.TrimRight(string(names[12+i*32:12+(i+1)*32]), "\x00"), Active: order.Uint32(bits[off+8:])&mask != 0, Fixed: !available || never, Available: available, Requested: order.Uint32(bits[off+4:])&mask != 0, NeverChanged: never})
	}
	return s
}
func find(s state) (int, error) {
	if s.FeatureError != "" {
		return -1, errors.New(s.FeatureError)
	}
	result := -1
	for i, f := range s.Features {
		if f.Name == "rx-gro-list" {
			if result >= 0 {
				return -1, errors.New("duplicate rx-gro-list name")
			}
			result = i
		}
	}
	if result < 0 {
		return -1, errors.New("rx-gro-list is unavailable")
	}
	if s.Features[result].Fixed {
		return -1, errors.New("rx-gro-list is fixed")
	}
	return result, nil
}
func setPayload(count, index int, value bool) ([]byte, error) {
	if count < 1 || count > 4096 || index < 0 || index >= count {
		return nil, errors.New("invalid feature index or count")
	}
	blocks := (count + 31) / 32
	buf := make([]byte, 8+blocks*8)
	order.PutUint32(buf, unix.ETHTOOL_SFEATURES)
	order.PutUint32(buf[4:], uint32(blocks))
	off := 8 + (index/32)*8
	mask := uint32(1) << uint(index%32)
	order.PutUint32(buf[off:], mask)
	if value {
		order.PutUint32(buf[off+4:], mask)
	}
	return buf, nil
}
func setOne(fd int, name string, count, index int, value bool) (uintptr, error) {
	if (name != "eth0" && name != "eth1") || index < 0 || index >= count {
		return 0, errors.New("SET whitelist rejected")
	}
	buf, err := setPayload(count, index, value)
	if err != nil {
		return 0, err
	}
	return ioctl(fd, name, buf)
}
func otherChanges(a, b state, index int) []string {
	out := []string{}
	if b.FeatureError != "" {
		return []string{"GET_after_failed"}
	}
	if len(a.Features) != len(b.Features) {
		return []string{"feature_set_changed"}
	}
	for i, f := range a.Features {
		g := b.Features[i]
		if f.Name != g.Name {
			out = append(out, "feature_order_changed")
			continue
		}
		if i != index && (f.Active != g.Active || f.Requested != g.Requested) {
			out = append(out, f.Name)
		}
	}
	return out
}
func main() {
	name := flag.String("interface", "", "GET interface (default eth0,eth1,br-lan); SET only eth0/eth1")
	set := flag.String("set-rx-gro-list", "", "explicitly request on or off; requires --apply")
	apply := flag.Bool("apply", false, "allow the single rx-gro-list SET request")
	selftest := flag.Bool("self-test", false, "check single-bit ABI encoding without opening a socket")
	flag.Parse()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	emit := func(v any) { _ = enc.Encode(v) }
	if *selftest {
		if flag.NArg() != 0 || *name != "" || *set != "" || *apply {
			emit(row{"error": "--self-test must be used alone"})
			os.Exit(2)
		}
		off, e1 := setPayload(65, 33, false)
		on, e2 := setPayload(65, 33, true)
		_, e3 := setPayload(65, 65, true)
		ok := e1 == nil && e2 == nil && e3 != nil && hex.EncodeToString(off) == "3b00000003000000000000000000000002000000000000000000000000000000" && hex.EncodeToString(on) == "3b00000003000000000000000000000002000000020000000000000000000000"
		emit(row{"self_test_passed": ok, "ifreq_bytes": unsafe.Sizeof(ifreq{}), "socket_opened": false, "ioctl_called": false, "checks": []string{"amd64_ifreq_layout", "single_valid_bit_only", "on_off_requested_bits", "out_of_range_rejected"}})
		if !ok {
			os.Exit(1)
		}
		return
	}
	if flag.NArg() != 0 || (*set == "" && *apply) || (*set != "" && (!*apply || (*set != "on" && *set != "off") || (*name != "eth0" && *name != "eth1"))) {
		emit(row{"error": "SET requires --interface eth0|eth1 --set-rx-gro-list on|off --apply; no other SET is permitted"})
		os.Exit(2)
	}
	if *name != "" && *name != "eth0" && *name != "eth1" && *name != "br-lan" {
		emit(row{"error": "interface must be eth0, eth1 or br-lan"})
		os.Exit(2)
	}
	fd, e := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if e != nil {
		emit(row{"error": "ioctl socket: " + e.Error()})
		os.Exit(1)
	}
	defer unix.Close(fd)
	if *set == "" {
		names := []string{"eth0", "eth1", "br-lan"}
		if *name != "" {
			names = []string{*name}
		}
		states := []state{}
		for _, n := range names {
			states = append(states, get(fd, n))
		}
		emit(row{"tool": "ua3f-ethtool-feature-v1", "read_only": true, "interfaces": states})
		return
	}
	before := get(fd, *name)
	index, e := find(before)
	if e != nil {
		emit(row{"error": e.Error(), "before": before})
		os.Exit(1)
	}
	value := *set == "on"
	original := before.Features[index]
	result := row{"tool": "ua3f-ethtool-feature-v1", "read_only": false, "only_requested_feature": "rx-gro-list", "interface": *name, "requested": value, "before": before}
	if original.Requested == value && original.Active == value {
		result["no_change_needed"] = true
		emit(result)
		return
	}
	ret, e := setOne(fd, *name, len(before.Features), index, value)
	result["set_return_flags"] = ret
	if e != nil {
		result["set_error"] = e.Error()
	}
	after := get(fd, *name)
	result["after"] = after
	others := otherChanges(before, after, index)
	result["other_feature_changes"] = others
	valid := e == nil && ret == 0 && after.FeatureError == "" && len(after.Features) > index && after.Features[index].Name == "rx-gro-list" && after.Features[index].Active == value && after.Features[index].Requested == value && len(others) == 0
	if !valid {
		_, restoreErr := setOne(fd, *name, len(before.Features), index, original.Requested)
		restored := get(fd, *name)
		result["error"] = "SET verification failed; attempted restoration of only rx-gro-list"
		if restoreErr != nil {
			result["restore_error"] = restoreErr.Error()
		}
		result["restored"] = restored
		result["restore_other_feature_changes"] = otherChanges(before, restored, index)
		result["restore_verified"] = restored.FeatureError == "" && len(restored.Features) > index && restored.Features[index] == original && len(otherChanges(before, restored, index)) == 0
		emit(result)
		os.Exit(1)
	}
	result["verified"] = true
	emit(result)
}
