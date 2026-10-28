// SPDX-License-Identifier: GPL-3.0-only
// Copyright (c) 2026 Lunaris617 and EgressVeil contributors.
//go:build linux

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Lunaris617/EgressVeil/core/internal/strict"
	"github.com/Lunaris617/EgressVeil/core/internal/transport"
)

var version = "0.1.0-dev"

func main() {
	ua := flag.String("ua", "FFF", "User-Agent for every parsed cleartext HTTP request")
	level := flag.String("log-level", "WARN", "DEBUG, INFO, WARN or ERROR; no request contents are logged")
	showVersion := flag.Bool("version", false, "show build version")
	flag.Parse()
	if *showVersion {
		fmt.Println("EgressVeil UA3F strict core", version)
		return
	}
	var verbosity slog.Level
	if flag.NArg() != 0 || !strict.ValidUserAgent(*ua) || verbosity.UnmarshalText([]byte(*level)) != nil {
		fmt.Fprintln(os.Stderr, "INVALID_CONFIGURATION")
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: verbosity})))
	// Preserve UA3F/OpenClash's established socket GID bypass. UID stays root
	// for IP_TRANSPARENT and SO_MARK; Setgid applies to all Go runtime threads.
	if err := syscall.Setgid(65534); err != nil {
		slog.Error("SOCKET_GROUP_SETUP_FAILED")
		os.Exit(1)
	}
	if err := os.WriteFile("/proc/self/oom_score_adj", []byte("-900"), 0); err != nil {
		slog.Warn("OOM_PRIORITY_NOT_APPLIED")
	}
	if err := run(*ua); err != nil {
		slog.Error("CORE_STOPPED", "reason", "listener_or_socket_failure")
		os.Exit(1)
	}
}

func run(ua string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	defer cancel()
	primary, err := transport.Listen(ctx, "0.0.0.0:1080")
	if err != nil {
		return err
	}
	defer primary.Close()
	final, err := transport.Listen(ctx, "0.0.0.0:1081")
	if err != nil {
		return err
	}
	defer final.Close()
	var mu sync.Mutex
	active := make(map[net.Conn]struct{})
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for c := range active {
			_ = c.Close()
		}
	}()
	track := func(c net.Conn) { mu.Lock(); active[c] = struct{}{}; mu.Unlock() }
	untrack := func(c net.Conn) { mu.Lock(); delete(active, c); mu.Unlock() }
	errorsCh := make(chan error, 2)
	accept := func(listener net.Listener, mark int) {
		for {
			client, e := listener.Accept()
			if e != nil {
				if ctx.Err() != nil || errors.Is(e, net.ErrClosed) {
					return
				}
				if errors.Is(e, syscall.EMFILE) || errors.Is(e, syscall.ENFILE) {
					time.Sleep(time.Second)
					continue
				}
				errorsCh <- e
				return
			}
			track(client)
			go func(c net.Conn) {
				defer c.Close()
				defer untrack(c)
				target := c.LocalAddr().String()
				if transport.IsLoopTarget(target) {
					slog.Debug("LOCAL_LISTENER_LOOP_REJECTED")
					return
				}
				upstream, e := transport.Connect(ctx, target, mark)
				if e != nil {
					slog.Debug("UPSTREAM_DIAL_FAILED")
					return
				}
				track(upstream)
				defer upstream.Close()
				defer untrack(upstream)
				if e = strict.RelayWithUA(c, upstream, ua); e != nil && !errors.Is(e, io.EOF) {
					slog.Debug("RELAY_CLOSED")
				}
			}(client)
		}
	}
	go accept(primary, transport.PrimaryMark)
	go accept(final, transport.FinalMark)
	slog.Info("STRICT_LISTENERS_READY")
	select {
	case <-ctx.Done():
		return nil
	case err := <-errorsCh:
		return err
	}
}
