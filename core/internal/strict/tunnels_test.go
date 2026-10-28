package strict_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

func TestSOCKSAndCONNECTTunnelsReclassifyServerFirstTLSAndH2(t *testing.T) {
	for _, transport := range []string{"SOCKS5", "CONNECT"} {
		for _, inner := range []string{"SSH", "TLS", "H2"} {
			t.Run(transport+"/"+inner, func(t *testing.T) {
				client, server, _ := relayPair(t)
				result := make(chan error, 1)
				go func() {
					if transport == "SOCKS5" {
						var greeting [3]byte
						if _, err := io.ReadFull(server, greeting[:]); err != nil {
							result <- err
							return
						}
						_, _ = server.Write([]byte{5, 0})
						var connect [10]byte
						if _, err := io.ReadFull(server, connect[:]); err != nil {
							result <- err
							return
						}
						_, _ = server.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
					} else {
						req, err := http.ReadRequest(bufio.NewReader(server))
						if err == nil {
							err = checkUA(req)
						}
						if err != nil {
							result <- err
							return
						}
						_, _ = io.WriteString(server, "HTTP/1.1 200 Connection Established\r\n\r\n")
					}
					switch inner {
					case "SSH":
						_, _ = io.WriteString(server, "SSH-2.0-tunnel-server\r\n")
						line, err := bufio.NewReader(server).ReadString('\n')
						if err == nil && line != "SSH-2.0-tunnel-client\r\n" {
							err = fmt.Errorf("inner SSH changed")
						}
						result <- err
					case "TLS":
						want := []byte{0x16, 3, 1, 0, 4, 1, 2, 3, 4}
						got := make([]byte, len(want))
						_, err := io.ReadFull(server, got)
						if err == nil && !bytes.Equal(want, got) {
							err = fmt.Errorf("inner TLS changed")
						}
						if err == nil {
							_, err = server.Write(got)
						}
						result <- err
					case "H2":
						(&http2.Server{}).ServeConn(server, &http2.ServeConnOpts{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { result <- checkUA(r); w.WriteHeader(204) })})
					}
				}()
				br := bufio.NewReader(client)
				if transport == "SOCKS5" {
					go func() { _, _ = client.Write([]byte{5, 1, 0}) }()
					var pair [2]byte
					if _, err := io.ReadFull(br, pair[:]); err != nil {
						t.Fatal(err)
					}
					go func() { _, _ = client.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1, 0, 80}) }()
					var reply [10]byte
					if _, err := io.ReadFull(br, reply[:]); err != nil {
						t.Fatal(err)
					}
				} else {
					go func() { _, _ = io.WriteString(client, "CONNECT inner.test:80 HTTP/1.1\r\nHost: inner.test:80\r\n\r\n") }()
					if _, err := http.ReadResponse(br, &http.Request{Method: "CONNECT"}); err != nil {
						t.Fatal(err)
					}
				}
				switch inner {
				case "SSH":
					line, err := br.ReadString('\n')
					if err != nil || line != "SSH-2.0-tunnel-server\r\n" {
						t.Fatalf("inner banner: %q %v", line, err)
					}
					_, _ = io.WriteString(client, "SSH-2.0-tunnel-client\r\n")
				case "TLS":
					want := []byte{0x16, 3, 1, 0, 4, 1, 2, 3, 4}
					go func() { _, _ = client.Write(want) }()
					got := make([]byte, len(want))
					if _, err := io.ReadFull(br, got); err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(want, got) {
						t.Fatal("inner TLS response changed")
					}
				case "H2":
					rd := make(chan error, 1)
					go func() {
						fr := http2.NewFramer(io.Discard, br)
						for {
							f, err := fr.ReadFrame()
							if err != nil {
								rd <- err
								return
							}
							if h, ok := f.(*http2.HeadersFrame); ok && h.StreamEnded() {
								rd <- nil
								return
							}
						}
					}()
					_, _ = io.WriteString(client, http2.ClientPreface)
					fr := http2.NewFramer(client, nil)
					_ = fr.WriteSettings()
					var b bytes.Buffer
					enc := hpack.NewEncoder(&b)
					for _, f := range []hpack.HeaderField{{Name: ":method", Value: "GET"}, {Name: ":scheme", Value: "http"}, {Name: ":authority", Value: "inner.test"}, {Name: ":path", Value: "/"}} {
						_ = enc.WriteField(f)
					}
					if err := fr.WriteHeaders(http2.HeadersFrameParam{StreamID: 1, BlockFragment: b.Bytes(), EndHeaders: true, EndStream: true}); err != nil {
						t.Fatal(err)
					}
					if err := <-rd; err != nil {
						t.Fatal(err)
					}
				}
				if err := <-result; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestH2ControlFramesAndResponsesRemainExact(t *testing.T) {
	client, server, _ := relayPair(t)
	var controls bytes.Buffer
	cf := http2.NewFramer(&controls, nil)
	_ = cf.WriteSettings(http2.Setting{ID: http2.SettingInitialWindowSize, Val: 100000})
	_ = cf.WritePing(false, [8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	// Exercise reusable-buffer growth and then a small frame after the large
	// unknown extension frame. Unknown types are legal to ignore/forward.
	_ = cf.WriteRawFrame(http2.FrameType(0xee), 0, 0, bytes.Repeat([]byte{0x5a}, 70000))
	_ = cf.WriteWindowUpdate(0, 65536)
	_ = cf.WriteRSTStream(1, http2.ErrCodeCancel)
	_ = cf.WriteGoAway(1, http2.ErrCodeNo, []byte("test"))
	var response bytes.Buffer
	rf := http2.NewFramer(&response, nil)
	_ = rf.WriteSettings()
	_ = rf.WritePing(true, [8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	_ = rf.WriteWindowUpdate(0, 123)
	result := make(chan error, 1)
	go func() {
		got := make([]byte, len(http2.ClientPreface)+controls.Len())
		_, err := io.ReadFull(server, got)
		want := append([]byte(http2.ClientPreface), controls.Bytes()...)
		if err == nil && !bytes.Equal(got, want) {
			err = fmt.Errorf("h2 controls changed")
		}
		if err == nil {
			_, err = server.Write(response.Bytes())
		}
		result <- err
	}()
	go func() { _, _ = io.WriteString(client, http2.ClientPreface); _, _ = client.Write(controls.Bytes()) }()
	got := make([]byte, response.Len())
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, response.Bytes()) {
		t.Fatal("h2 responses changed")
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestOpaqueLargeBidirectionalPrefixPreserved(t *testing.T) {
	client, server, _ := relayPair(t)
	request := make([]byte, 2*1024*1024+5)
	copy(request, []byte{0x16, 3, 3, 0, 1})
	for i := 5; i < len(request); i++ {
		request[i] = byte(i * 17)
	}
	response := make([]byte, 2*1024*1024+3)
	for i := range response {
		response[i] = byte(i * 31)
	}
	result := make(chan error, 1)
	go func() {
		got := make([]byte, len(request))
		_, err := io.ReadFull(server, got)
		if err == nil && !bytes.Equal(got, request) {
			err = fmt.Errorf("opaque upload changed")
		}
		if err == nil {
			_, err = server.Write(response)
		}
		result <- err
	}()
	written := make(chan error, 1)
	go func() { _, err := client.Write(request); written <- err }()
	got := make([]byte, len(response))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, response) {
		t.Fatal("opaque download prefix/payload changed")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestInvalidH2HeadersDoNotLeak(t *testing.T) {
	for _, fragment := range [][]byte{{0xff}, {0, 20, 'u', 's', 'e', 'r', '-', 'a', 'g', 'e', 'n', 't'}} {
		t.Run(fmt.Sprint(len(fragment)), func(t *testing.T) {
			client, server, done := relayPair(t)
			got := make(chan []byte, 1)
			go func() { b, _ := io.ReadAll(server); got <- b }()
			_, _ = io.WriteString(client, http2.ClientPreface)
			fr := http2.NewFramer(client, nil)
			_ = fr.WriteSettings()
			_ = fr.WriteHeaders(http2.HeadersFrameParam{StreamID: 1, BlockFragment: fragment, EndStream: true, EndHeaders: true})
			if err := <-done; err == nil {
				t.Fatal("invalid HPACK accepted")
			}
			b := <-got
			var settings bytes.Buffer
			_ = http2.NewFramer(&settings, nil).WriteSettings()
			want := append([]byte(http2.ClientPreface), settings.Bytes()...)
			if !bytes.Equal(b, want) {
				t.Fatalf("invalid HEADERS leaked, got %d bytes", len(b))
			}
		})
	}
}

func TestReusedHTTPConnectionParseFailureCannotBecomeOpaque(t *testing.T) {
	client, server, done := relayPair(t)
	tail := make(chan []byte, 1)
	go func() {
		br := bufio.NewReader(server)
		req, err := http.ReadRequest(br)
		if err != nil || checkUA(req) != nil {
			tail <- []byte("initial request failed")
			return
		}
		_, _ = io.WriteString(server, "HTTP/1.1 204 No Content\r\n\r\n")
		b, _ := io.ReadAll(br)
		tail <- b
	}()
	go func() { _, _ = io.WriteString(client, "GET /first HTTP/1.1\r\nHost: x\r\n\r\n") }()
	if _, err := http.ReadResponse(bufio.NewReader(client), nil); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(client, "GET /bad HTTP/1.1\r\nBroken field\r\nUser-Agent: must-not-leak\r\n\r\n")
	if err := <-done; err == nil {
		t.Fatal("expected parse rejection")
	}
	if got := <-tail; len(got) != 0 {
		t.Fatalf("unparsed reused request leaked %d bytes", len(got))
	}
}
