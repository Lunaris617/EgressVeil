//go:build linux

// Read only audit. All Docker replies and command lines remain in RAM.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type report struct {
	Known       bool   `json:"known_topology"`
	Ready       bool   `json:"bridge_path_ready"`
	DockerPID   int    `json:"docker_pid,omitempty"`
	DockerState string `json:"docker_state"`
	Reason      string `json:"reason"`
	GlobalV4    string `json:"global_ipv4"`
	LANV4       string `json:"lan_ipv4"`
	DockerV4    string `json:"docker_ipv4"`
}

func read(p string) string {
	b, e := os.ReadFile(p)
	if e != nil {
		return "missing"
	}
	return strings.TrimSpace(string(b))
}
func pid() (int, error) {
	b, e := exec.Command("ubus", "call", "service", "list", `{"name":"dockerd"}`).Output()
	if e != nil {
		return 0, errors.New("docker_procd_unavailable")
	}
	var v map[string]struct {
		Instances map[string]struct {
			PID     int  `json:"pid"`
			Running bool `json:"running"`
		} `json:"instances"`
	}
	if json.Unmarshal(b, &v) != nil {
		return 0, errors.New("docker_procd_invalid")
	}
	p := 0
	for _, i := range v["dockerd"].Instances {
		if i.Running && i.PID > 0 {
			if p != 0 {
				return 0, errors.New("docker_instances_unknown")
			}
			p = i.PID
		}
	}
	// A non-procd daemon must never be mistaken for a stopped service.
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return 0, errors.New("process_scan_unavailable")
	}
	seen := 0
	for _, entry := range entries {
		n, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile("/proc/" + entry.Name() + "/comm")
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, errors.New("process_scan_incomplete")
		}
		exe, _ := os.Readlink("/proc/" + entry.Name() + "/exe")
		if strings.TrimSpace(string(comm)) == "dockerd" || filepath.Base(strings.TrimSuffix(exe, " (deleted)")) == "dockerd" {
			if n != p || seen != 0 {
				return 0, errors.New("docker_unmanaged_process")
			}
			seen = n
		}
	}
	if p != seen {
		return 0, errors.New("docker_procd_process_mismatch")
	}
	if p > 0 && read("/proc/"+strconv.Itoa(p)+"/comm") != "dockerd" {
		return 0, errors.New("docker_process_identity_unknown")
	}
	return p, nil
}
func docker(path string, out any) error {
	tr := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}}
	defer tr.CloseIdleConnections()
	c := http.Client{Transport: tr, Timeout: 2 * time.Second}
	r, e := c.Get("http://docker" + path)
	if e != nil {
		return errors.New("docker_api_unavailable")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return errors.New("docker_api_status")
	}
	if json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(out) != nil {
		return errors.New("docker_api_invalid")
	}
	return nil
}
func audit(expected int, stopped bool) report {
	r := report{Reason: "unknown", GlobalV4: read("/proc/sys/net/bridge/bridge-nf-call-iptables"), LANV4: read("/sys/class/net/"+lanBridge+"/bridge/nf_call_iptables"), DockerV4: read("/sys/class/net/docker0/bridge/nf_call_iptables")}
	reject := func(s string) report { r.Reason = s; return r }
	links, e := os.ReadDir("/sys/class/net")
	if e != nil {
		return reject("links_unreadable")
	}
	names := []string{}
	for _, l := range links {
		if _, e := os.Stat(filepath.Join("/sys/class/net", l.Name(), "bridge")); e == nil {
			names = append(names, l.Name())
		}
	}
	sort.Strings(names)
	bridgeNames:=[]string{lanBridge,"docker0"};sort.Strings(bridgeNames)
	if strings.Join(names, ",") != strings.Join(bridgeNames,",") && strings.Join(names, ",") != lanBridge {
		return reject("bridge_topology_unknown")
	}
	members, e := os.ReadDir("/sys/class/net/"+lanBridge+"/brif")
	if e != nil || len(members) != 1 || members[0].Name() != lanPort {
		return reject("lan_members_unknown")
	}
	dockerPresent := r.DockerV4 != "missing"
	var b []byte
	if dockerPresent {
		b, e = exec.Command("ip", "-d", "-j", "link", "show", "master", "docker0").Output()
		if e != nil {
			return reject("docker_members_unreadable")
		}
		var ports []struct {
			Info struct {
				Kind string `json:"info_kind"`
			} `json:"linkinfo"`
		}
		if json.Unmarshal(b, &ports) != nil {
			return reject("docker_members_invalid")
		}
		for _, p := range ports {
			if p.Info.Kind != "veth" {
				return reject("docker_member_kind_changed_revalidation_required")
			}
		}
	}
	if read("/proc/sys/net/bridge/bridge-nf-call-ip6tables") != "1" {
		return reject("ipv6_baseline_changed")
	}
	if r.LANV4 != "0" {
		return reject("lan_bridge_flag_changed")
	}
	p, e := pid()
	if e != nil {
		return reject(e.Error())
	}
	r.DockerPID = p
	r.DockerState = "running"
	if stopped && p != 0 {
		return reject("docker_still_running")
	}
	if p == 0 {
		r.DockerState = "stopped"
		if expected > 0 {
			return reject("docker_pid_changed")
		}
		again, err := pid()
		if err != nil || again != 0 {
			return reject("docker_stopped_state_changed")
		}
		r.Known = true
		r.Ready = r.GlobalV4 == "0" && r.LANV4 == "0" && (!dockerPresent || r.DockerV4 == "1")
		r.Reason = "stopped_safe_waiting_for_bridge_policy"
		if r.Ready {
			r.Reason = "stopped_safe_ready"
		}
		return r
	}
	if !dockerPresent {
		return reject("running_docker_bridge_missing")
	}
	if expected > 0 && p != expected {
		return reject("docker_pid_changed")
	}
	b, e = os.ReadFile("/proc/" + strconv.Itoa(p) + "/cmdline")
	if e != nil {
		return reject("docker_cmdline_unavailable")
	}
	args := strings.Split(string(b), "\x00")
	config := "/etc/docker/daemon.json"
	for i, a := range args {
		if strings.HasPrefix(a, "--config-file=") {
			config = strings.TrimPrefix(a, "--config-file=")
		}
		if a == "--config-file" && i+1 < len(args) {
			config = args[i+1]
		}
		for _, k := range []string{"--userland-proxy", "--iptables", "--ip6tables"} {
			want := "true"
			if k == "--ip6tables" {
				want = "false"
			}
			if strings.HasPrefix(a, k+"=") && strings.TrimPrefix(a, k+"=") != want {
				return reject("docker_command_policy_unknown")
			}
			if a == k && i+1 < len(args) && args[i+1] != "" && !strings.HasPrefix(args[i+1], "-") && args[i+1] != want {
				return reject("docker_command_policy_unknown")
			}
		}
	}
	if config != "/tmp/dockerd/daemon.json" && config != "/etc/docker/daemon.json" {
		return reject("docker_config_location_unknown")
	}
	b, e = os.ReadFile(config)
	if e != nil {
		return reject("docker_config_unavailable")
	}
	var cfg struct {
		IPTables  *bool `json:"iptables"`
		IP6Tables *bool `json:"ip6tables"`
		Proxy     *bool `json:"userland-proxy"`
	}
	if json.Unmarshal(b, &cfg) != nil || cfg.IPTables == nil || !*cfg.IPTables || cfg.IP6Tables == nil || *cfg.IP6Tables || (cfg.Proxy != nil && !*cfg.Proxy) {
		return reject("docker_config_policy_unknown")
	}
	var ver struct {
		Version string `json:"Version"`
		Commit  string `json:"GitCommit"`
	}
	if e = docker("/version", &ver); e != nil {
		return reject(e.Error())
	}
	if ver.Version != "29.6.1" || !strings.HasPrefix(ver.Commit, "8ec5ab3") {
		return reject("docker_version_changed_revalidation_required")
	}
	var networks []struct {
		Name    string            `json:"Name"`
		Driver  string            `json:"Driver"`
		Options map[string]string `json:"Options"`
	}
	if e = docker("/networks", &networks); e != nil {
		return reject(e.Error())
	}
	count := 0
	for _, n := range networks {
		if n.Driver != "bridge" {
			continue
		}
		count++
		bridgeName := n.Options["com.docker.network.bridge.name"]
		if bridgeName != "docker0" && !(bridgeName == "" && n.Name == "bridge") {
			return reject("docker_network_changed_revalidation_required")
		}
		if n.Options["com.docker.network.bridge.enable_icc"] != "true" || n.Options["com.docker.network.bridge.enable_ip_masquerade"] != "true" {
			return reject("docker_network_policy_changed_revalidation_required")
		}
	}
	if count != 1 {
		return reject("docker_network_count_changed_revalidation_required")
	}
	for _, args := range [][]string{{"-C", "FORWARD", "-j", "DOCKER-USER"}, {"-C", "FORWARD", "-j", "DOCKER-FORWARD"}, {"-C", "DOCKER-FORWARD", "-j", "DOCKER-CT"}, {"-C", "DOCKER-FORWARD", "-j", "DOCKER-INTERNAL"}, {"-C", "DOCKER-FORWARD", "-j", "DOCKER-BRIDGE"}, {"-C", "DOCKER-FORWARD", "-i", "docker0", "-j", "ACCEPT"}, {"-C", "DOCKER-BRIDGE", "-o", "docker0", "-j", "DOCKER"}, {"-S", "DOCKER-USER"}} {
		if exec.Command("iptables", append([]string{"-t", "filter"}, args...)...).Run() != nil {
			return reject("docker_filter_path_changed_revalidation_required")
		}
	}
	again, e := pid()
	if e != nil || again != p {
		return reject("docker_pid_changed")
	}
	r.Known = true
	r.Ready = r.GlobalV4 == "0" && r.LANV4 == "0" && r.DockerV4 == "1"
	r.Reason = "known_waiting_for_bridge_policy"
	if r.Ready {
		r.Reason = "ready"
	}
	return r
}
func main() {
 if e:=loadRuntimeTopology();e!=nil{os.Stdout.WriteString(`{"known_topology":false,"error":"TOPOLOGY_NOT_READY"}`);os.Exit(1)}
	mode := flag.String("mode", "known", "known|ready|pid")
	expected := flag.Int("pid", 0, "expected dockerd PID")
	stopped := flag.Bool("require-stopped", false, "require no dockerd process")
	flag.Parse()
	if *mode == "pid" {
		p, e := pid()
		if e != nil {
			os.Exit(1)
		}
		os.Stdout.WriteString(strconv.Itoa(p) + "\n")
		return
	}
	if *mode != "known" && *mode != "ready" {
		os.Exit(2)
	}
	r := audit(*expected, *stopped)
	json.NewEncoder(os.Stdout).Encode(r)
	if !r.Known || (*mode == "ready" && !r.Ready) {
		os.Exit(1)
	}
}
