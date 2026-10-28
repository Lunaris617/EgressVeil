package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	op := ""
	if len(os.Args) == 2 {
		op = os.Args[1]
	}
	if op != "barrier" && op != "apply" && op != "check" && op != "ready" && op != "remove" {
		fmt.Println(`{"strict_ready":false,"error":"usage: ttl-policy barrier|apply|check|ready|remove"}`)
		os.Exit(2)
	}
	if e:=loadRuntimeTopology(); e!=nil { fmt.Println(`{"strict_ready":false,"error":"TOPOLOGY_NOT_READY"}`);os.Exit(1) }
	r, e := execute(op)
	if e != nil {
		r["error"] = e.Error()
		r["strict_ready"] = false
	}
	_ = json.NewEncoder(os.Stdout).Encode(r)
	if e != nil {
		os.Exit(1)
	}
}
