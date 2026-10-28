package strict_test

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/net/http2/hpack"
)

type singleListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
	addr  net.Addr
}

func (l *singleListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *singleListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *singleListener) Addr() net.Addr { return l.addr }

func TestH2CTraditionalUpgradeAgainstMatureServer(t *testing.T) {
	client, server, _ := relayPair(t)
	listener := &singleListener{conns: make(chan net.Conn, 1), done: make(chan struct{}), addr: server.LocalAddr()}
	listener.conns <- server
	seen := make(chan error, 2)
	srv := &http.Server{Handler: h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- checkUA(r); w.WriteHeader(204) }), &http2.Server{})}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close(); _ = listener.Close() })
	go func() {
		_, _ = io.WriteString(client, "GET /first HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade, HTTP2-Settings\r\nUpgrade: h2c\r\nHTTP2-Settings: AAMAAABk\r\nUser-Agent: first-old\r\n\r\n")
	}()
	br := bufio.NewReader(client)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("upgrade status %d", resp.StatusCode)
	}
	readDone := make(chan error, 1)
	go func() {
		fr := http2.NewFramer(io.Discard, br)
		ended := map[uint32]bool{}
		for len(ended) < 2 {
			f, err := fr.ReadFrame()
			if err != nil {
				readDone <- err
				return
			}
			switch f := f.(type) {
			case *http2.HeadersFrame:
				if f.StreamEnded() {
					ended[f.StreamID] = true
				}
			case *http2.DataFrame:
				if f.StreamEnded() {
					ended[f.StreamID] = true
				}
			}
		}
		readDone <- nil
	}()
	_, _ = io.WriteString(client, http2.ClientPreface)
	fw := http2.NewFramer(client, nil)
	_ = fw.WriteSettings()
	var block bytes.Buffer
	enc := hpack.NewEncoder(&block)
	for _, f := range []hpack.HeaderField{{Name: ":method", Value: "GET"}, {Name: ":scheme", Value: "http"}, {Name: ":authority", Value: "example.test"}, {Name: ":path", Value: "/second"}, {Name: "user-agent", Value: "second-old"}} {
		_ = enc.WriteField(f)
	}
	if err := fw.WriteHeaders(http2.HeadersFrameParam{StreamID: 3, BlockFragment: block.Bytes(), EndStream: true, EndHeaders: true}); err != nil {
		t.Fatal(err)
	}
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := <-seen; err != nil {
			t.Fatal(err)
		}
	}
}
