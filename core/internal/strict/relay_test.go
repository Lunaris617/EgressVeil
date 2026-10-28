package strict_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lunaris617/EgressVeil/core/internal/strict"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

func relayPair(t *testing.T) (net.Conn, net.Conn, <-chan error) {
	return relayPairWithUA(t, "FFF")
}

func relayPairWithUA(t *testing.T, userAgent string) (net.Conn, net.Conn, <-chan error) {
	t.Helper()
	client, left := net.Pipe()
	right, server := net.Pipe()
	deadline := time.Now().Add(8 * time.Second)
	for _, c := range []net.Conn{client, left, right, server} {
		_ = c.SetDeadline(deadline)
		t.Cleanup(func() { _ = c.Close() })
	}
	done := make(chan error, 1)
	go func() { done <- strict.RelayWithUA(left, right, userAgent) }()
	return client, server, done
}

func TestServerFirstBannersAndNonHTTPTextRemainTransparent(t *testing.T) {
	for _, tc := range []struct{ name, banner, command string }{
		{"ssh", "SSH-2.0-test-server\r\n", "SSH-2.0-test-client\r\n"},
		{"smtp", "220 mail.test ESMTP ready\r\n", "EHLO client.test\r\n"},
		{"ftp", "220 FTP ready\r\n", "USER test\r\n"},
		{"sip", "", "INVITE sip:user@example.test SIP/2.0\r\nVia: SIP/2.0/TCP client.test\r\nUser-Agent: preserve-non-http\r\n\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server, _ := relayPair(t)
			result := make(chan error, 1)
			go func() {
				if tc.banner != "" {
					if _, err := io.WriteString(server, tc.banner); err != nil {
						result <- err
						return
					}
				}
				got := make([]byte, len(tc.command))
				_, err := io.ReadFull(server, got)
				if err == nil && string(got) != tc.command {
					err = fmt.Errorf("non-HTTP command changed")
				}
				if err == nil {
					_, err = server.Write(got)
				}
				result <- err
			}()
			if tc.banner != "" {
				got := make([]byte, len(tc.banner))
				if _, err := io.ReadFull(client, got); err != nil {
					t.Fatal(err)
				}
				if string(got) != tc.banner {
					t.Fatal("banner changed")
				}
			}
			go func() { _, _ = io.WriteString(client, tc.command) }()
			got := make([]byte, len(tc.command))
			if _, err := io.ReadFull(client, got); err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.command {
				t.Fatal("non-HTTP response changed")
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConfiguredUAAppliesToHTTP1AndH2ThenRestoreFFF(t *testing.T) {
	for _, ua := range []string{"AuditUA-Setting", "FFF"} {
		t.Run(ua+"/HTTP1", func(t *testing.T) {
			client, server, _ := relayPairWithUA(t, ua)
			result := make(chan error, 1)
			go func() {
				req, err := http.ReadRequest(bufio.NewReader(server))
				if err == nil && (len(req.Header.Values("User-Agent")) != 1 || req.UserAgent() != ua) {
					err = fmt.Errorf("configured HTTP1 UA did not apply")
				}
				result <- err
				_, _ = io.WriteString(server, "HTTP/1.1 204 No Content\r\n\r\n")
			}()
			go func() { _, _ = io.WriteString(client, "GET / HTTP/1.1\r\nHost: x\r\nUser-Agent: old\r\n\r\n") }()
			if _, err := http.ReadResponse(bufio.NewReader(client), nil); err != nil {
				t.Fatal(err)
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
		t.Run(ua+"/H2", func(t *testing.T) {
			client, server, _ := relayPairWithUA(t, ua)
			result := make(chan error, 1)
			go (&http2.Server{}).ServeConn(server, &http2.ServeConnOpts{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if len(r.Header.Values("User-Agent")) != 1 || r.UserAgent() != ua {
					result <- fmt.Errorf("configured H2 UA did not apply")
				} else {
					result <- nil
				}
				w.WriteHeader(204)
			})})
			readDone := make(chan error, 1)
			go func() {
				fr := http2.NewFramer(io.Discard, client)
				for {
					f, err := fr.ReadFrame()
					if err != nil {
						readDone <- err
						return
					}
					if f, ok := f.(*http2.HeadersFrame); ok && f.StreamEnded() {
						readDone <- nil
						return
					}
				}
			}()
			_, _ = io.WriteString(client, http2.ClientPreface)
			fr := http2.NewFramer(client, nil)
			_ = fr.WriteSettings()
			var b bytes.Buffer
			enc := hpack.NewEncoder(&b)
			for _, f := range []hpack.HeaderField{{Name: ":method", Value: "GET"}, {Name: ":scheme", Value: "http"}, {Name: ":authority", Value: "x"}, {Name: ":path", Value: "/"}, {Name: "user-agent", Value: "old"}} {
				_ = enc.WriteField(f)
			}
			if err := fr.WriteHeaders(http2.HeadersFrameParam{StreamID: 1, BlockFragment: b.Bytes(), EndStream: true, EndHeaders: true}); err != nil {
				t.Fatal(err)
			}
			if err := <-readDone; err != nil {
				t.Fatal(err)
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func checkUA(req *http.Request) error {
	if got := req.Header.Values("User-Agent"); len(got) != 1 || got[0] != "FFF" {
		return fmt.Errorf("UA = %q", got)
	}
	return nil
}

func TestHTTP1SegmentedPipelinedBodiesAndEveryUA(t *testing.T) {
	client, server, _ := relayPair(t)
	cases := []string{
		"GET /empty HTTP/1.0\r\nHost: st.dl.eccdnx.com\r\n\r\n",
		"GET /blank HTTP/1.1\r\nHost: example.test\r\nUser-Agent:\r\n\r\n",
		"POST /duplicate HTTP/1.1\r\nHost: example.test\r\nuSeR-aGeNt: old\r\nUSER-AGENT: second\r\nContent-Length: 7\r\n\r\npayload",
		"POST /chunk HTTP/1.1\r\nHost: example.test\r\nUser-Agent: old\r\n continuation\r\nTransfer-Encoding: chunked\r\nTrailer: User-Agent, X-End\r\n\r\n4;ext=yes\r\nbody\r\n0\r\nUser-Agent: trailer-old\r\nX-End: retained\r\n\r\n",
		"GET /reuse HTTP/1.1\r\nHost: example.test\r\n\r\n",
	}
	result := make(chan error, 1)
	go func() {
		br := bufio.NewReader(server)
		for i := range cases {
			req, err := http.ReadRequest(br)
			if err != nil {
				result <- err
				return
			}
			if err = checkUA(req); err != nil {
				result <- err
				return
			}
			if i == 0 && req.Proto != "HTTP/1.0" {
				result <- fmt.Errorf("version changed")
				return
			}
			data, err := io.ReadAll(req.Body)
			if err != nil {
				result <- err
				return
			}
			if i == 2 && string(data) != "payload" || i == 3 && string(data) != "body" {
				result <- fmt.Errorf("body changed")
				return
			}
			if i == 3 && (req.Trailer.Get("User-Agent") != "" || req.Trailer.Get("X-End") != "retained") {
				result <- fmt.Errorf("bad trailers")
				return
			}
			if _, err = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"); err != nil {
				result <- err
				return
			}
		}
		result <- nil
	}()
	writeDone := make(chan error, 1)
	go func() {
		for _, wire := range cases {
			for i := range wire {
				if _, err := client.Write([]byte{wire[i]}); err != nil {
					writeDone <- err
					return
				}
			}
		}
		writeDone <- nil
	}()
	br := bufio.NewReader(client)
	for range cases {
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(resp.Body)
		if err != nil || string(b) != "OK" {
			t.Fatalf("response: %q %v", b, err)
		}
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
}

func TestIdentifiedHTTPParseFailureDoesNotLeak(t *testing.T) {
	for _, bad := range []string{
		"GET / HTTP/1.1\r\nBroken field\r\n\r\nSECRET",
		"POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 6\r\nTransfer-Encoding: chunked\r\n\r\nSECRET",
		"GET / HTTP/1.1\r\nHost: x\r\nUser-Agent: leak\x00\r\n\r\nSECRET",
	} {
		t.Run(fmt.Sprintf("case%d", len(bad)), func(t *testing.T) {
			client, server, done := relayPair(t)
			got := make(chan []byte, 1)
			go func() { b, _ := io.ReadAll(server); got <- b }()
			_, _ = io.WriteString(client, bad)
			if err := <-done; err == nil {
				t.Error("expected rejection")
			}
			if b := <-got; len(b) != 0 {
				t.Fatalf("unparsed request leaked %d bytes", len(b))
			}
		})
	}
}

func TestTLSAndBinaryRemainByteExact(t *testing.T) {
	for _, payload := range [][]byte{{0x16, 3, 1, 0, 8, 1, 2, 3, 4, 5}, []byte("SSH-2.0-example\r\n"), {0, 1, 2, 3, 4}} {
		t.Run(fmt.Sprintf("first%d", payload[0]), func(t *testing.T) {
			client, server, _ := relayPair(t)
			result := make(chan error, 1)
			go func() {
				got := make([]byte, len(payload))
				_, err := io.ReadFull(server, got)
				if err == nil && !bytes.Equal(got, payload) {
					err = fmt.Errorf("bytes changed")
				}
				if err == nil {
					_, err = server.Write(got)
				}
				result <- err
			}()
			go func() { _, _ = client.Write(payload) }()
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(client, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatal("response changed")
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCONNECTReclassifiesInnerHTTP(t *testing.T) {
	client, server, _ := relayPair(t)
	result := make(chan error, 1)
	go func() {
		br := bufio.NewReader(server)
		req, err := http.ReadRequest(br)
		if err != nil {
			result <- err
			return
		}
		if err = checkUA(req); err != nil {
			result <- err
			return
		}
		if _, err = io.WriteString(server, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			result <- err
			return
		}
		req, err = http.ReadRequest(br)
		if err != nil {
			result <- err
			return
		}
		if err = checkUA(req); err != nil {
			result <- err
			return
		}
		_, err = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK")
		result <- err
	}()
	go func() { _, _ = io.WriteString(client, "CONNECT host.test:80 HTTP/1.1\r\nHost: host.test:80\r\n\r\n") }()
	br := bufio.NewReader(client)
	resp, err := http.ReadResponse(br, &http.Request{Method: "CONNECT"})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("connect %v", err)
	}
	go func() {
		_, _ = io.WriteString(client, "GET /nested HTTP/1.1\r\nHost: host.test\r\nUser-Agent: secret-old\r\n\r\n")
	}()
	resp, err = http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestSOCKS5ConnectThenHTTP(t *testing.T) {
	for _, auth := range []bool{false, true} {
		t.Run(fmt.Sprint(auth), func(t *testing.T) {
			client, server, _ := relayPair(t)
			result := make(chan error, 1)
			go func() {
				greeting := make([]byte, 3)
				if _, err := io.ReadFull(server, greeting); err != nil {
					result <- err
					return
				}
				method := byte(0)
				if auth {
					method = 2
				}
				if !bytes.Equal(greeting, []byte{5, 1, method}) {
					result <- fmt.Errorf("greeting changed")
					return
				}
				_, _ = server.Write([]byte{5, method})
				if auth {
					// Synthetic test bytes only; no real credentials enter this test.
					var data [5]byte
					if _, err := io.ReadFull(server, data[:]); err != nil {
						result <- err
						return
					}
					if !bytes.Equal(data[:], []byte{1, 1, 'u', 1, 'p'}) {
						result <- fmt.Errorf("authentication changed")
						return
					}
					_, _ = server.Write([]byte{1, 0})
				}
				var connect [10]byte
				if _, err := io.ReadFull(server, connect[:]); err != nil {
					result <- err
					return
				}
				_, _ = server.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
				req, err := http.ReadRequest(bufio.NewReader(server))
				if err == nil {
					err = checkUA(req)
				}
				if err != nil {
					result <- err
					return
				}
				_, err = io.WriteString(server, "HTTP/1.1 204 No Content\r\n\r\n")
				result <- err
			}()
			method := byte(0)
			if auth {
				method = 2
			}
			go func() { _, _ = client.Write([]byte{5, 1, method}) }()
			var pair [2]byte
			if _, err := io.ReadFull(client, pair[:]); err != nil {
				t.Fatal(err)
			}
			if auth {
				go func() { _, _ = client.Write([]byte{1, 1, 'u', 1, 'p'}) }()
				if _, err := io.ReadFull(client, pair[:]); err != nil {
					t.Fatal(err)
				}
			}
			go func() { _, _ = client.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1, 0, 80}) }()
			var reply [10]byte
			if _, err := io.ReadFull(client, reply[:]); err != nil {
				t.Fatal(err)
			}
			go func() { _, _ = io.WriteString(client, "GET / HTTP/1.1\r\nHost: x\r\n\r\n") }()
			if _, err := http.ReadResponse(bufio.NewReader(client), nil); err != nil {
				t.Fatal(err)
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestH2PriorKnowledgeRealServerMultiplexContinuationAndDynamicHPACK(t *testing.T) {
	client, server, _ := relayPair(t)
	seen := make(chan error, 3)
	h2s := &http2.Server{}
	go h2s.ServeConn(server, &http2.ServeConnOpts{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := checkUA(r)
		data, e := io.ReadAll(r.Body)
		if err == nil {
			err = e
		}
		if r.URL.Path == "/body" && string(data) != "streamed-body" {
			err = fmt.Errorf("h2 body changed")
		}
		if r.URL.Path == "/large" && len(r.Header.Get("X-Large")) != 60000 {
			err = fmt.Errorf("h2 large header changed")
		}
		seen <- err
		_, _ = io.WriteString(w, "OK")
	})})
	responses := make(chan error, 1)
	go func() {
		fr := http2.NewFramer(io.Discard, client)
		ended := 0
		for ended < 3 {
			f, err := fr.ReadFrame()
			if err != nil {
				responses <- err
				return
			}
			if f, ok := f.(*http2.DataFrame); ok && f.StreamEnded() {
				ended++
			}
		}
		responses <- nil
	}()
	if _, err := io.WriteString(client, http2.ClientPreface); err != nil {
		t.Fatal(err)
	}
	fw := http2.NewFramer(client, nil)
	if err := fw.WriteSettings(); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	enc := hpack.NewEncoder(&compressed)
	for i, path := range []string{"/body", "/duplicate", "/large"} {
		compressed.Reset()
		fields := []hpack.HeaderField{{Name: ":method", Value: "POST"}, {Name: ":scheme", Value: "http"}, {Name: ":authority", Value: "example.test"}, {Name: ":path", Value: path}, {Name: "x-reuse", Value: "dynamic-entry"}}
		if i == 1 {
			fields = append(fields, hpack.HeaderField{Name: "user-agent", Value: "old-one"}, hpack.HeaderField{Name: "User-Agent", Value: "old-two"})
		}
		if i == 2 {
			fields = append(fields, hpack.HeaderField{Name: "user-agent", Value: ""}, hpack.HeaderField{Name: "x-large", Value: strings.Repeat("x", 60000)})
		}
		for _, f := range fields {
			if err := enc.WriteField(f); err != nil {
				t.Fatal(err)
			}
		}
		block := append([]byte(nil), compressed.Bytes()...)
		id := uint32(i*2 + 1)
		split := len(block) / 2
		// Fragment every header block across HEADERS/CONTINUATION, splitting HPACK.
		if split > 16000 {
			split = 16000
		}
		if err := fw.WriteHeaders(http2.HeadersFrameParam{StreamID: id, BlockFragment: block[:split], EndStream: i != 0, EndHeaders: false, Priority: http2.PriorityParam{Weight: 10}}); err != nil {
			t.Fatal(err)
		}
		rest := block[split:]
		for len(rest) > 0 {
			n := len(rest)
			if n > 16000 {
				n = 16000
			}
			if err := fw.WriteContinuation(id, n == len(rest), rest[:n]); err != nil {
				t.Fatal(err)
			}
			rest = rest[n:]
		}
	}
	if err := fw.WriteData(1, true, []byte("streamed-body")); err != nil {
		t.Fatal(err)
	}
	if err := <-responses; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := <-seen; err != nil {
			t.Fatal(err)
		}
	}
}

func TestH2UpgradeNormalizesFirstAndLaterRequests(t *testing.T) {
	client, server, _ := relayPair(t)
	result := make(chan error, 1)
	go func() {
		br := bufio.NewReader(server)
		req, err := http.ReadRequest(br)
		if err == nil {
			err = checkUA(req)
		}
		if err != nil {
			result <- err
			return
		}
		_, _ = io.WriteString(server, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: h2c\r\n\r\n")
		pre := make([]byte, len(http2.ClientPreface))
		if _, err = io.ReadFull(br, pre); err != nil {
			result <- err
			return
		}
		if string(pre) != http2.ClientPreface {
			result <- fmt.Errorf("preface changed")
			return
		}
		fr := http2.NewFramer(io.Discard, br)
		decoder := hpack.NewDecoder(4096, nil)
		for {
			f, e := fr.ReadFrame()
			if e != nil {
				result <- e
				return
			}
			if h, ok := f.(*http2.HeadersFrame); ok {
				fields, e := decoder.DecodeFull(h.HeaderBlockFragment())
				if e != nil {
					result <- e
					return
				}
				count := 0
				for _, f := range fields {
					if strings.EqualFold(f.Name, "user-agent") {
						count++
						if f.Value != "FFF" {
							result <- fmt.Errorf("bad upgraded UA")
							return
						}
					}
				}
				if count != 1 {
					result <- fmt.Errorf("UA count %d", count)
					return
				}
				result <- nil
				return
			}
		}
	}()
	go func() {
		_, _ = io.WriteString(client, "GET /upgrade HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade, HTTP2-Settings\r\nUpgrade: h2c\r\nHTTP2-Settings: AAMAAABk\r\n\r\n")
	}()
	if _, err := http.ReadResponse(bufio.NewReader(client), nil); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(client, http2.ClientPreface)
	fr := http2.NewFramer(client, nil)
	_ = fr.WriteSettings()
	var b bytes.Buffer
	enc := hpack.NewEncoder(&b)
	for _, f := range []hpack.HeaderField{{Name: ":method", Value: "GET"}, {Name: ":scheme", Value: "http"}, {Name: ":authority", Value: "x"}, {Name: ":path", Value: "/second"}} {
		_ = enc.WriteField(f)
	}
	if err := fr.WriteHeaders(http2.HeadersFrameParam{StreamID: 3, BlockFragment: b.Bytes(), EndStream: true, EndHeaders: true}); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestParallelConnectionsDoNotShareProtocolState(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, server, _ := relayPair(t)
			go func() {
				req, err := http.ReadRequest(bufio.NewReader(server))
				if err != nil {
					return
				}
				if checkUA(req) != nil {
					t.Error("shared state UA")
				}
				_, _ = io.WriteString(server, "HTTP/1.1 204 No Content\r\n\r\n")
			}()
			go func() { _, _ = io.WriteString(client, "GET / HTTP/1.1\r\nHost: x\r\nUser-Agent: varying\r\n\r\n") }()
			if _, err := http.ReadResponse(bufio.NewReader(client), nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
