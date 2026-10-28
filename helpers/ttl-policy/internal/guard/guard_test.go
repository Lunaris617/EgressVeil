package guard

import (
	"encoding/json"
	"testing"
)

func fixture() map[string]any {
	e := []any{}
	for i, k := range []string{"nfproto", "l4proto", "iifname", "oifname", "iifname"} {
		op := "=="
		if i == 4 {
			op = "!="
		}
		e = append(e, map[string]any{"match": map[string]any{"op": op, "left": map[string]any{"meta": map[string]any{"key": k}}, "right": []any{"ipv4", "tcp", "br-lan", "eth1", "@ready_ifnames"}[i]}})
	}
	e = append(e, map[string]any{"counter": map[string]any{"packets": 0, "bytes": 0}}, map[string]any{"drop": nil})
	return map[string]any{"nftables": []any{
		map[string]any{"table": map[string]any{"family": "inet", "name": Table, "comment": Owner}},
		map[string]any{"set": map[string]any{"family": "inet", "table": Table, "name": "ready_ifnames", "type": "ifname"}},
		map[string]any{"chain": map[string]any{"family": "inet", "table": Table, "name": "forward", "type": "filter", "hook": "forward", "prio": 105, "policy": "accept"}},
		map[string]any{"rule": map[string]any{"family": "inet", "table": Table, "chain": "forward", "comment": Owner + ":blocked", "expr": e}},
	}}
}
func TestGateShape(t *testing.T) {
	f := fixture()
	b, _ := json.Marshal(f)
	s, e := Parse(b)
	if e != nil || !s.Valid || s.Ready {
		t.Fatal(s, e)
	}
	set := f["nftables"].([]any)[1].(map[string]any)["set"].(map[string]any)
	set["elem"] = []string{"br-lan"}
	b, _ = json.Marshal(f)
	s, e = Parse(b)
	if e != nil || !s.Ready {
		t.Fatal(s, e)
	}
	set["elem"] = []string{"eth0"}
	b, _ = json.Marshal(f)
	if _, e = Parse(b); e == nil {
		t.Fatal("unexpected interface accepted")
	}
	delete(set, "elem")
	chain := f["nftables"].([]any)[2].(map[string]any)["chain"].(map[string]any)
	chain["prio"] = 0
	b, _ = json.Marshal(f)
	if _, e = Parse(b); e == nil {
		t.Fatal("wrong priority accepted")
	}
	// A malformed right operand must be rejected without panicking.
	chain["prio"] = 105
	r := f["nftables"].([]any)[3].(map[string]any)["rule"].(map[string]any)
	r["expr"].([]any)[0].(map[string]any)["match"].(map[string]any)["right"] = map[string]any{"unexpected": true}
	b, _ = json.Marshal(f)
	if _, e = Parse(b); e == nil {
		t.Fatal("malformed operand accepted")
	}
}
