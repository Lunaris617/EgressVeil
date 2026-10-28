package main

import (
	"testing"
)

func TestTopologyAndOffloadRejections(t *testing.T) {
	b := []byte("network.lan=interface\nnetwork.lan.device='br-lan'\nnetwork.wan=interface\nnetwork.wan.device='eth1'\nnetwork.cfg123=device\nnetwork.cfg123.name='br-lan'\nnetwork.cfg123.type='bridge'\nnetwork.cfg123.ports='eth0'\n")
	m, e := parseUCI(b)
	if e != nil || validateNetwork(m) != nil {
		t.Fatal("expected fixed topology accepted", e)
	}
	for _, bad := range []string{"eth0 eth2", "eth1", "eth0\\x"} {
		m["network.cfg123.ports"] = bad
		if validateNetwork(m) == nil {
			t.Fatal("unexpected member accepted", bad)
		}
	}
	f, e := parseUCI([]byte("firewall.cfg1=defaults\nfirewall.cfg1.flow_offloading='0'\nfirewall.cfg1.flow_offloading_hw='0'\n"))
	if e != nil || auditUCI(f) != nil {
		t.Fatal(e)
	}
	f["firewall.cfg1.flow_offloading"] = "1"
	if auditUCI(f) == nil {
		t.Fatal("offload accepted")
	}
	if auditNFT([]byte(`{"nftables":[{"flowtable":{"name":"ft"}}]}`)) == nil {
		t.Fatal("nft flowtable accepted")
	}
	if auditNFT([]byte(`{"nftables":[{"rule":{"expr":[{"flow":{"op":"add","flowtable":"ft"}}]}}]}`)) == nil {
		t.Fatal("flow add accepted")
	}
	if auditModules([]byte("shortcut_fe 100 0 - Live 0\n")) == nil {
		t.Fatal("accelerator accepted")
	}
}

func TestFeatureProof(t *testing.T) {
	good := `{"interfaces":[{"interface":"eth0","driver":"r8169","features":[{"name":"rx-gro","active":true,"requested":true},{"name":"rx-gro-list","active":false,"requested":false}]}]}`
	if _, e := inspectFeature([]byte(good), true); e != nil {
		t.Fatal(e)
	}
	bad := []string{
		`{"interfaces":[{"interface":"eth0","driver":"r8169","features":[{"name":"rx-gro","active":false},{"name":"rx-gro-list"}]}]}`,
		`{"interfaces":[{"interface":"eth0","driver":"r8169","features":[{"name":"rx-gro","active":true},{"name":"rx-gro-list","requested":true}]}]}`,
		`{"interfaces":[{"interface":"eth1","driver":"r8169","features":[{"name":"rx-gro","active":true},{"name":"rx-gro-list"}]}]}`,
	}
	for _, b := range bad {
		if _, e := inspectFeature([]byte(b), true); e == nil {
			t.Fatal("unsafe feature proof accepted")
		}
	}
}
