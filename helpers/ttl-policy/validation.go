package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type feature struct {
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	Requested bool   `json:"requested"`
}
type state struct {
	Interface    string    `json:"interface"`
	Driver       string    `json:"driver"`
	FeatureError string    `json:"feature_error"`
	DriverError  string    `json:"driver_error"`
	Features     []feature `json:"features"`
}
type featureProof struct {
	Driver        string `json:"driver"`
	GRO           bool   `json:"rx_gro_active"`
	ListActive    bool   `json:"rx_gro_list_active"`
	ListRequested bool   `json:"rx_gro_list_requested"`
}

func inspectFeature(b []byte, desired bool) (featureProof, error) {
	var d struct {
		Interfaces []state `json:"interfaces"`
	}
	if json.Unmarshal(b, &d) != nil || len(d.Interfaces) != 1 {
		return featureProof{}, errors.New("ethtool GET JSON invalid")
	}
	s := d.Interfaces[0]
	p := featureProof{Driver: s.Driver}
	if s.Interface != lanPort || !supportedDriver(s.Driver) || s.DriverError != "" || s.FeatureError != "" {
		return p, errors.New("eth0 r8169 feature GET failed")
	}
	gro, list := 0, 0
	for _, f := range s.Features {
		switch f.Name {
		case "rx-gro":
			gro++
			p.GRO = f.Active
		case "rx-gro-list":
			list++
			p.ListActive = f.Active
			p.ListRequested = f.Requested
		}
	}
	if gro != 1 || list != 1 || !p.GRO {
		return p, errors.New("ordinary GRO must remain active and both features must exist uniquely")
	}
	if desired && (p.ListActive || p.ListRequested) {
		return p, errors.New("rx-gro-list active/requested must both be off")
	}
	return p, nil
}

// Only a tiny fixed subset is retained. UCI output is never logged or persisted.
func parseUCI(b []byte) (map[string]string, error) {
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i < 1 {
			return nil, errors.New("UCI output malformed")
		}
		key, val := line[:i], line[i+1:]
		parts := strings.Split(key, ".")
		if len(parts) < 2 || len(parts) > 3 {
			continue
		}
		if len(parts) == 3 && parts[2] != "type" && parts[2] != "name" && parts[2] != "ports" && parts[2] != "device" && parts[2] != "flow_offloading" && parts[2] != "flow_offloading_hw" {
			continue
		}
		if strings.ContainsAny(val, "\r\x00") {
			return nil, errors.New("UCI selected value malformed")
		}
		// uci show wraps string/list members in single quotes. Selected values
		// must be simple names; escaping or complex values is rejected later.
		val = strings.ReplaceAll(val, "'", "")
		if _, ok := m[key]; ok {
			return nil, errors.New("UCI duplicate selected key")
		}
		m[key] = val
	}
	return m, nil
}

var simpleSection = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func validateNetwork(m map[string]string) error {
	if m["network.lan.device"] != lanBridge || m["network.wan.device"] != wanDevice {
		return errors.New("UCI LAN/WAN must be br-lan/eth1")
	}
	n := 0
	for k, v := range m {
		if !strings.HasSuffix(k, ".name") || v != lanBridge {
			continue
		}
		p := strings.TrimSuffix(k, ".name")
		s := strings.TrimPrefix(p, "network.")
		if !simpleSection.MatchString(s) || m[p] != "device" || m[p+".type"] != "bridge" || strings.Join(strings.Fields(m[p+".ports"]), " ") != lanPort {
			return errors.New("UCI br-lan must be a bridge with only eth0")
		}
		n++
	}
	if n != 1 {
		return errors.New("UCI requires exactly one br-lan bridge device")
	}
	return nil
}

func auditUCI(m map[string]string) error {
	defaults := 0
	for k, v := range m {
		if !strings.Contains(strings.TrimPrefix(k, "firewall."), ".") && v == "defaults" {
			defaults++
		}
		if strings.HasSuffix(k, ".flow_offloading") || strings.HasSuffix(k, ".flow_offloading_hw") {
			if v != "0" && v != "false" && v != "off" {
				return errors.New("UCI flow offload is enabled or unknown")
			}
		}
	}
	if defaults != 1 {
		return errors.New("firewall default section is missing or ambiguous")
	}
	return nil
}

func auditNFT(b []byte) error {
	var d map[string]any
	if json.Unmarshal(b, &d) != nil {
		return errors.New("nft ruleset JSON invalid")
	}
	if _, ok := d["nftables"].([]any); !ok {
		return errors.New("nft ruleset objects missing")
	}
	var walk func(any) error
	walk = func(x any) error {
		switch v := x.(type) {
		case map[string]any:
			for k, y := range v {
				if k == "flowtable" || k == "flow" || k == "flow_offload" {
					return errors.New("nft flow offload declaration or expression present")
				}
				if k == "name" {
					if n, ok := y.(string); ok && strings.EqualFold(n, "FLOWOFFLOAD") {
						return errors.New("xt FLOWOFFLOAD compatibility target present")
					}
				}
				if e := walk(y); e != nil {
					return e
				}
			}
		case []any:
			for _, y := range v {
				if e := walk(y); e != nil {
					return e
				}
			}
		}
		return nil
	}
	return walk(d)
}

func auditModules(b []byte) error {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		n := strings.ToLower(f[0])
		for _, s := range []string{"shortcut", "fast_classifier", "sfe_", "ecm", "nss_", "hnat", "mtk_hnat", "fast_nat"} {
			if strings.Contains(n, s) {
				return fmt.Errorf("unreviewed accelerator module present: %s", n)
			}
		}
	}
	return nil
}
