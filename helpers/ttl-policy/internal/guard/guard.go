// Package guard owns exactly one nft table. It never changes another table.
package guard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"time"
)

const Table = "ua3fi_ttl_guard"
const Owner = "ua3f-ttl-policy:v1"
var LANInterface = "br-lan"
var WANInterface = "eth1"
var Definition = `table inet ua3fi_ttl_guard {
 comment "ua3f-ttl-policy:v1"
 set ready_ifnames { type ifname; }
 chain forward {
  type filter hook forward priority 105; policy accept;
  meta nfproto ipv4 meta l4proto tcp iifname "br-lan" oifname "eth1" iifname != @ready_ifnames counter drop comment "ua3f-ttl-policy:v1:blocked"
 }
}
`

type Snapshot struct {
	Exists bool `json:"exists"`
	Owned  bool `json:"owned"`
	Valid  bool `json:"valid"`
	Ready  bool `json:"ready"`
}

// Run deliberately discards stderr: nft may include configuration in diagnostics.
func Run(path, input string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, path, args...)
	if input != "" {
		c.Stdin = bytes.NewBufferString(input)
	}
	b, e := c.Output()
	if ctx.Err() != nil {
		return nil, errors.New("command timed out")
	}
	if e != nil {
		return nil, fmt.Errorf("%s command failed", path)
	}
	if len(b) > 32<<20 {
		return nil, errors.New("command output exceeds bound")
	}
	return b, nil
}

func Inspect(path string) (Snapshot, error) {
	b, e := Run(path, "", "-j", "list", "tables")
	if e != nil {
		return Snapshot{}, e
	}
	var doc struct {
		Nftables []map[string]json.RawMessage `json:"nftables"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return Snapshot{}, errors.New("nft tables JSON invalid")
	}
	found := false
	for _, o := range doc.Nftables {
		if t, ok := o["table"]; ok {
			var x struct{ Family, Name string }
			if json.Unmarshal(t, &x) != nil {
				return Snapshot{}, errors.New("nft table JSON invalid")
			}
			if x.Family == "inet" && x.Name == Table {
				found = true
			}
		}
	}
	if !found {
		return Snapshot{}, nil
	}
	b, e = Run(path, "", "-j", "list", "table", "inet", Table)
	if e != nil {
		return Snapshot{Exists: true}, e
	}
	return Parse(b)
}

func Parse(b []byte) (Snapshot, error) {
	s := Snapshot{Exists: true}
	var doc struct {
		Nftables []map[string]json.RawMessage `json:"nftables"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return s, errors.New("nft table JSON invalid")
	}
	nt, nc, ns, nr := 0, 0, 0, 0
	valid := true
	for _, obj := range doc.Nftables {
		for kind, raw := range obj {
			if kind == "metainfo" {
				continue
			}
			var m map[string]any
			if json.Unmarshal(raw, &m) != nil {
				return s, errors.New("nft object JSON invalid")
			}
			if m["family"] != "inet" {
				valid = false
			}
			switch kind {
			case "table":
				nt++
				s.Owned = m["name"] == Table && m["comment"] == Owner
			case "chain":
				nc++
				valid = valid && m["table"] == Table && m["name"] == "forward" && m["type"] == "filter" && m["hook"] == "forward" && m["prio"] == float64(105) && m["policy"] == "accept"
			case "set":
				ns++
				valid = valid && m["table"] == Table && m["name"] == "ready_ifnames" && m["type"] == "ifname"
				if f, ok := m["flags"]; ok && !reflect.DeepEqual(f, []any{}) {
					valid = false
				}
				if e, ok := m["elem"]; ok {
					a, ok := e.([]any)
					if !ok || len(a) > 1 {
						valid = false
					} else if len(a) == 1 {
						if a[0] == LANInterface {
							s.Ready = true
						} else {
							valid = false
						}
					}
				}
				for _, k := range []string{"timeout", "gc-interval", "policy", "size"} {
					if _, ok := m[k]; ok {
						valid = false
					}
				}
			case "rule":
				nr++
				valid = valid && m["table"] == Table && m["chain"] == "forward" && m["comment"] == Owner+":blocked" && validRule(m["expr"])
			default:
				valid = false
			}
		}
	}
	s.Valid = valid && s.Owned && nt == 1 && nc == 1 && ns == 1 && nr == 1
	if !s.Valid {
		return s, errors.New("owned nft gate shape is not the exact expected policy")
	}
	return s, nil
}

func validRule(v any) bool {
	a, ok := v.([]any)
	if !ok || len(a) != 7 {
		return false
	}
	keys := []string{"nfproto", "l4proto", "iifname", "oifname", "iifname"}
	rights := []any{"ipv4", "tcp", LANInterface, WANInterface, "@ready_ifnames"}
	for i := 0; i < 5; i++ {
		m, ok := a[i].(map[string]any)
		if !ok || len(m) != 1 {
			return false
		}
		x, ok := m["match"].(map[string]any)
		if !ok || len(x) != 3 {
			return false
		}
		op := "=="
		if i == 4 {
			op = "!="
		}
		if x["op"] != op {
			return false
		}
		want := map[string]any{"meta": map[string]any{"key": keys[i]}}
		if !reflect.DeepEqual(x["left"], want) {
			return false
		}
		r := x["right"]
		if i == 0 && r == float64(2) {
			r = "ipv4"
		}
		if i == 1 && r == float64(6) {
			r = "tcp"
		}
		if !reflect.DeepEqual(r, rights[i]) {
			return false
		}
	}
	c, ok := a[5].(map[string]any)
	if !ok || len(c) != 1 {
		return false
	}
	if _, ok = c["counter"]; !ok {
		return false
	}
	d, ok := a[6].(map[string]any)
	if !ok || len(d) != 1 {
		return false
	}
	_, ok = d["drop"]
	return ok
}

// Close creates an empty gate before any topology or feature work. Unknown ownership
// is never removed. A damaged but owned table is atomically rebuilt closed.
func Close(path string) error {
	s, e := Inspect(path)
	if e != nil && !s.Exists {
		return e
	}
	if s.Exists && !s.Owned {
		return errors.New("gate table exists without our ownership marker")
	}
	input := ""
	if s.Exists && s.Valid && e == nil {
		input = "flush set inet " + Table + " ready_ifnames\n"
	} else {
		if s.Exists {
			input = "delete table inet " + Table + "\n"
		}
		input += Definition
	}
	if _, e = Run(path, input, "-f", "-"); e != nil {
		return e
	}
	s, e = Inspect(path)
	if e != nil {
		return e
	}
	if !s.Valid || s.Ready {
		return errors.New("closed gate verification failed")
	}
	return nil
}

func Open(path string) error {
	s, e := Inspect(path)
	if e != nil {
		return e
	}
	if !s.Valid || s.Ready {
		return errors.New("opening requires a valid empty gate")
	}
	if _, e = Run(path, "add element inet "+Table+" ready_ifnames { \""+LANInterface+"\" }\n", "-f", "-"); e != nil {
		return e
	}
	s, e = Inspect(path)
	if e != nil || !s.Ready {
		return errors.New("ready gate verification failed")
	}
	return nil
}

func Remove(path string) error {
	s, e := Inspect(path)
	if !s.Exists && e == nil {
		return nil
	}
	if !s.Owned {
		return errors.New("refusing to delete table without our owner marker")
	}
	_, e = Run(path, "delete table inet "+Table+"\n", "-f", "-")
	return e
}
