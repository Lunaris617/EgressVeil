//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"ua3f-ttl-policy/internal/guard"
)

const helperPath = "/usr/libexec/ua3f-integration/ethtool-feature"
const helperSHA = "f65ba4069f40542d00a9c942cb8bbe85d1a8cc9e0baa18fce93c23e8215e0895"
const stateDir = "/var/run/ua3f-ttl-policy"

type topology struct {
	Eth0   int `json:"eth0_ifindex"`
	WAN    int `json:"eth1_ifindex"`
	Bridge int `json:"br_lan_ifindex"`
	Master int `json:"eth0_master_ifindex"`
}

func fixedTool(name string) (string, error) {
	for _, d := range []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"} {
		p := d + "/" + name
		if s, e := os.Stat(p); e == nil && s.Mode().IsRegular() && s.Mode().Perm()&0111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("required tool %s missing", name)
}

func requireProduction() error {
	if os.Geteuid() != 0 {
		return errors.New("root required")
	}
	a, e := os.Stat("/proc/self/ns/net")
	if e != nil {
		return errors.New("cannot inspect current network namespace")
	}
	b, e := os.Stat("/proc/1/ns/net")
	if e != nil || !os.SameFile(a, b) {
		return errors.New("production policy requires PID1 network namespace; use separate fixture for lab")
	}
	return nil
}

func secureFile(p string, directory bool) error {
	s, e := os.Lstat(p)
	if e != nil {
		return e
	}
	st, ok := s.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || s.Mode()&os.ModeSymlink != 0 || s.Mode().Perm()&0022 != 0 || s.IsDir() != directory || (!directory && !s.Mode().IsRegular()) {
		return errors.New("policy file ownership/type/permissions rejected")
	}
	return nil
}

func lock() (*os.File, error) {
	if e := os.Mkdir(stateDir, 0700); e != nil && !os.IsExist(e) {
		return nil, e
	}
	if e := secureFile(stateDir, true); e != nil {
		return nil, e
	}
	fd, e := unix.Open(stateDir+"/lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), stateDir+"/lock")
	if e = secureFile(f.Name(), false); e != nil {
		f.Close()
		return nil, e
	}
	if e = unix.Flock(fd, unix.LOCK_EX); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}

func getUCI(uci, pkg string) (map[string]string, error) {
	b, e := guard.Run(uci, "", "-q", "-X", "show", pkg)
	if e != nil {
		return nil, fmt.Errorf("UCI %s read failed", pkg)
	}
	return parseUCI(b)
}

func getTopology(uci string) (topology, error) {
	t := topology{}
	m, e := getUCI(uci, "network")
	if e != nil {
		return t, e
	}
	if e = validateNetwork(m); e != nil {
		return t, e
	}
	a, e := netlink.LinkByName(lanPort)
	if e != nil {
		return t, errors.New("eth0 absent")
	}
	w, e := netlink.LinkByName(wanDevice)
	if e != nil {
		return t, errors.New("eth1 absent")
	}
	if a.Type() != "device" || w.Type() != "device" {
		return t, errors.New("eth0/eth1 must be physical device links")
	}
	t.Eth0 = a.Attrs().Index
	t.WAN = w.Attrs().Index
	t.Master = a.Attrs().MasterIndex
	drv, e := filepath.EvalSymlinks("/sys/class/net/"+lanPort+"/device/driver")
	if e != nil || !supportedDriver(filepath.Base(drv)) {
		return t, errors.New("eth0 driver is not r8169")
	}
	br, e := netlink.LinkByName(lanBridge)
	if e != nil {
		var absent netlink.LinkNotFoundError
		if !errors.As(e, &absent) {
			return t, errors.New("br-lan query failed")
		}
		if t.Master != 0 {
			return t, errors.New("eth0 already enslaved to an unknown bridge")
		}
		return t, nil
	}
	if br.Type() != "bridge" {
		return t, errors.New("br-lan is not a bridge")
	}
	t.Bridge = br.Attrs().Index
	if t.Master != 0 && t.Master != t.Bridge {
		return t, errors.New("eth0 master is not br-lan")
	}
	links, e := netlink.LinkList()
	if e != nil {
		return t, errors.New("link inventory unavailable")
	}
	for _, l := range links {
		if l.Attrs().MasterIndex == t.Bridge && l.Attrs().Name != lanPort {
			return t, errors.New("br-lan has an unapproved runtime member")
		}
	}
	return t, nil
}

func auditOffload(nft, uci string) error {
	m, e := getUCI(uci, "firewall")
	if e != nil {
		return e
	}
	if e = auditUCI(m); e != nil {
		return e
	}
	b, e := guard.Run(nft, "", "-j", "list", "ruleset")
	if e != nil {
		return errors.New("nft ruleset offload audit unavailable")
	}
	if e = auditNFT(b); e != nil {
		return e
	}
	b, e = os.ReadFile("/proc/modules")
	if e != nil {
		return errors.New("module audit unavailable")
	}
	if e = auditModules(b); e != nil {
		return e
	}
	for _, name := range []string{lanPort, wanDevice, lanBridge} {
		l, e := netlink.LinkByName(name)
		if e != nil {
			var absent netlink.LinkNotFoundError
			if name == lanBridge && errors.As(e, &absent) {
				continue
			}
			return errors.New("offload link query failed")
		}
		if x := l.Attrs().Xdp; x != nil && (x.Attached || x.ProgId != 0 || x.AttachMode != 0) {
			return errors.New("XDP is attached on a policy interface")
		}
		for _, at := range []uint32{unix.BPF_TCX_INGRESS, unix.BPF_TCX_EGRESS} {
			// Read-only BPF_PROG_QUERY prefix. Zero buffers request only count.
			a := struct {
				Target, Attach, Query, Flags uint32
				IDs                          uint64
				Count, Pad                   uint32
			}{Target: uint32(l.Attrs().Index), Attach: at}
			_, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_QUERY, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a))
			if errno != 0 {
				return errors.New("TCX attachment audit unavailable")
			}
			if a.Count != 0 {
				return errors.New("TCX program attached on policy interface")
			}
		}
		qs, e := netlink.QdiscList(l)
		if e != nil {
			return errors.New("qdisc inventory unavailable")
		}
		parents := map[uint32]bool{netlink.HANDLE_ROOT: true, netlink.HANDLE_INGRESS: true, netlink.HANDLE_MIN_INGRESS: true, netlink.HANDLE_MIN_EGRESS: true}
		for _, q := range qs {
			if b := q.Attrs().IngressBlock; b != nil && *b != 0 {
				return errors.New("unreviewed shared TC block")
			}
			parents[q.Attrs().Handle] = true
		}
		for p := range parents {
			fs, e := netlink.FilterList(l, p)
			if e != nil && !errors.Is(e, unix.EOPNOTSUPP) {
				return errors.New("TC filter audit unavailable")
			}
			if len(fs) != 0 {
				return errors.New("unreviewed TC filters on policy interface")
			}
		}
	}
	return nil
}

func verifyHelper() error {
	if e := secureFile(helperPath, false); e != nil {
		return errors.New("fixed ethtool helper permissions/type rejected")
	}
	b, e := os.ReadFile(helperPath)
	if e != nil {
		return errors.New("fixed ethtool helper unreadable")
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != helperSHA {
		return errors.New("ethtool helper SHA256 is not frozen f65 build")
	}
	return nil
}

func features(desired bool) (featureProof, error) {
	if e := verifyHelper(); e != nil {
		return featureProof{}, e
	}
	b, e := guard.Run(helperPath, "", "--interface", lanPort)
	if e != nil {
		return featureProof{}, errors.New("ethtool GET failed")
	}
	return inspectFeature(b, desired)
}

func setFeature() error {
	if e := verifyHelper(); e != nil {
		return e
	}
	b, e := guard.Run(helperPath, "", "--interface", lanPort, "--set-rx-gro-list", "off", "--apply")
	if e != nil {
		return errors.New("single-feature SET failed; gate remains closed")
	}
	var r struct {
		Verified bool     `json:"verified"`
		NoChange bool     `json:"no_change_needed"`
		Error    string   `json:"error"`
		Other    []string `json:"other_feature_changes"`
	}
	if json.Unmarshal(b, &r) != nil || r.Error != "" || len(r.Other) != 0 || (!r.Verified && !r.NoChange) {
		return errors.New("frozen helper did not verify isolated single-feature SET")
	}
	return nil
}

func check(nft, uci string) (map[string]any, error) {
	r := map[string]any{"strict_ready": false, "scope": "IPv4 TCP br-lan -> eth1", "read_only": true}
	s, e := guard.Inspect(nft)
	r["gate"] = s
	if e != nil {
		return r, e
	}
	if !s.Valid || !s.Ready {
		return r, errors.New("native TCP gate is closed or absent")
	}
	t, e := getTopology(uci)
	r["topology"] = t
	if e != nil {
		return r, e
	}
	p, e := features(true)
	r["feature"] = p
	if e != nil {
		return r, e
	}
	if e = auditOffload(nft, uci); e != nil {
		return r, e
	}
	r["offload_audit"] = "no nft flow offload, enabled UCI offload, known accelerator modules, interface XDP or TC filters"
	s, e = guard.Inspect(nft)
	if e != nil || !s.Valid || !s.Ready {
		return r, errors.New("gate changed during check")
	}
	t2, e := getTopology(uci)
	if e != nil || t2 != t {
		return r, errors.New("topology changed during check")
	}
	r["strict_ready"] = true
	return r, nil
}

func execute(op string) (map[string]any, error) {
	r := map[string]any{"tool": "ua3f-ttl-policy-v1", "operation": op, "strict_ready": false}
	if e := requireProduction(); e != nil {
		return r, e
	}
	if e := netlink.SetSocketTimeout(8 * time.Second); e != nil {
		return r, errors.New("netlink timeout setup failed")
	}
	nft, e := fixedTool("nft")
	if e != nil {
		return r, e
	}
	if op == "check" || op == "ready" {
		uci, e := fixedTool("uci")
		if e != nil {
			return r, e
		}
		c, e := check(nft, uci)
		for k, v := range c {
			r[k] = v
		}
		return r, e
	}
	f, e := lock()
	if e != nil {
		return r, e
	}
	defer f.Close()
	if op == "remove" {
		e = guard.Remove(nft)
		if e == nil { e = restoreOwnedFeature() }
		if e == nil {
			_ = os.Remove(stateDir + "/status.json")
			_ = os.Remove(stateDir + "/status.new")
		}
		r["removed"] = e == nil
		r["feature_restored"] = e == nil
		return r, e
	}
	if e = guard.Close(nft); e != nil {
		return r, e
	}
	r["gate_closed"] = true
	if op == "barrier" {
		return r, nil
	}
	// Every failure from here retains the empty gate, including post-open checks.
	opened := false
	success := false
	defer func() {
		if opened && !success {
			e := guard.Close(nft)
			r["gate_closed"] = e == nil
			if e != nil {
				r["gate_close_error"] = e.Error()
			}
		}
	}()
	uci, e := fixedTool("uci")
	if e != nil {
		return r, e
	}
	t, e := getTopology(uci)
	r["topology"] = t
	if e != nil {
		return r, e
	}
	if e = auditOffload(nft, uci); e != nil {
		return r, e
	}
	p, e := features(false)
	r["before"] = p
	if e != nil {
		return r, e
	}
	if e = rememberFeature(p); e != nil { return r, e }
	if e = setFeature(); e != nil {
		return r, e
	}
	p, e = features(true)
	r["after"] = p
	if e != nil {
		return r, e
	}
	t2, e := getTopology(uci)
	if e != nil || t2 != t {
		return r, errors.New("interface identity changed during feature application")
	}
	if e = auditOffload(nft, uci); e != nil {
		return r, e
	}
	// Mark opened before attempting Open so an incomplete verification closes it.
	opened = true
	r["gate_closed"] = false
	if e = guard.Open(nft); e != nil {
		return r, e
	}
	c, e := check(nft, uci)
	if e != nil {
		return r, e
	}
	for k, v := range c {
		r[k] = v
	}
	r["read_only"] = false
	success = true
	// State is diagnostic only. Readiness never trusts a file instead of fresh GET.
	state, _ := json.Marshal(r)
	tmp := stateDir + "/status.new"
	if e = os.WriteFile(tmp, append(state, '\n'), 0600); e == nil {
		_ = os.Rename(tmp, stateDir+"/status.json")
	}
	return r, nil
}
