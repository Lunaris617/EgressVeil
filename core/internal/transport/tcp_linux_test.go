// SPDX-License-Identifier: GPL-3.0-only
//go:build linux

package transport

import (
	"context"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"sync"
	"testing"
)

func TestMarksRemainPerConnection(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root/CAP_NET_ADMIN for SO_MARK")
	}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			c.Close()
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := PrimaryMark
			if i%2 == 1 {
				want = FinalMark
			}
			c, e := Connect(context.Background(), l.Addr().String(), want)
			if e != nil {
				t.Error(e)
				return
			}
			defer c.Close()
			raw, e := c.(*net.TCPConn).SyscallConn()
			if e != nil {
				t.Error(e)
				return
			}
			var got int
			var se error
			e = raw.Control(func(fd uintptr) { got, se = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK) })
			if e != nil || se != nil || got != want {
				t.Errorf("mark got %x want %x: %v %v", got, want, e, se)
			}
		}(i)
	}
	wg.Wait()
}

func TestLoopTargets(t *testing.T) {
	if !IsLoopTarget("127.0.0.1:1080") || !IsLoopTarget("127.0.0.1:1081") {
		t.Fatal("local listener must be rejected")
	}
	if IsLoopTarget("127.0.0.1:80") || IsLoopTarget("203.0.113.19:1080") {
		t.Fatal("ordinary target must not be treated as a loop")
	}
}
