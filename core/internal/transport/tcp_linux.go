// SPDX-License-Identifier: GPL-3.0-only
// Derived from UA3F's IP_TRANSPARENT listener and per-connection SO_MARK path.
// Copyright (c) UA3F contributors; EgressVeil modifications (c) 2026 Lunaris617.
//go:build linux

package transport

import (
	"context"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const PrimaryMark = 0xc9
const FinalMark = 0x1cb

func Listen(ctx context.Context, address string) (net.Listener, error) {
	lc := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var socketErr error
		err := raw.Control(func(fd uintptr) {
			if socketErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); socketErr == nil {
				socketErr = unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_TRANSPARENT, 1)
			}
		})
		if err != nil {
			return err
		}
		return socketErr
	}}
	return lc.Listen(ctx, "tcp4", address)
}

// Each dial captures its mark. No shared mutable outbound route exists.
func Connect(ctx context.Context, address string, mark int) (net.Conn, error) {
	d := net.Dialer{Timeout: 10 * time.Second, Control: func(_, _ string, raw syscall.RawConn) error {
		var socketErr error
		err := raw.Control(func(fd uintptr) { socketErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark) })
		if err != nil {
			return err
		}
		return socketErr
	}}
	return d.DialContext(ctx, "tcp4", address)
}

// TPROXY preserves the original target in the accepted socket's local address.
// Only destinations that actually point at one of our local listeners loop.
func IsLoopTarget(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return true
	}
	if port != "1080" && port != "1081" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() {
		return true
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return true
	}
	for _, addr := range addresses {
		if local, _, e := net.ParseCIDR(addr.String()); e == nil && local.Equal(ip) {
			return true
		}
	}
	return false
}
