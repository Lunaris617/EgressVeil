package strict_test

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLeadingEmptyCRLFHTTPRewritesUA(t *testing.T) {
	for _, prefix := range []string{"\r\n", "\r\n\r\n", strings.Repeat("\r\n", 128)} {
		client, server, _ := relayPair(t)
		go func() {
			_, _ = io.WriteString(client, prefix+"GET / HTTP/1.1\r\nHost: audit.invalid\r\nUser-Agent: AuditUA-Prefix\r\n\r\n")
		}()
		req, err := http.ReadRequest(bufio.NewReader(server))
		if err != nil {
			t.Fatal(err)
		}
		if req.UserAgent() != "FFF" {
			t.Fatal("leading empty-line HTTP UA escaped")
		}
	}
}

func TestLeadingLFAndWhitespaceRequestLinesNeverFallback(t *testing.T) {
	for _, start := range []string{
		"\nGET / HTTP/1.1", "\r\n\nGET / HTTP/1.1", "\rGET / HTTP/1.1", "\r\r\nGET / HTTP/1.1",
		"GET\t/ HTTP/1.1", "GET /\tHTTP/1.1", "GET\v/ HTTP/1.1", "GET /\fHTTP/1.1", "GET\r/ HTTP/1.1",
	} {
		t.Run(strings.ReplaceAll(start, "\n", "LF"), func(t *testing.T) {
			client, server, done := relayPair(t)
			go func() {
				_, _ = io.WriteString(client, start+"\r\nHost: audit.invalid\r\nUser-Agent: AuditUA-Prefix\r\n\r\n")
			}()
			got, err := io.ReadAll(server)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 0 {
				t.Fatal("ambiguous HTTP request was copied raw")
			}
			if err = <-done; err == nil {
				t.Fatal("expected strict rejection")
			}
		})
	}
}

func TestLeadingCRLFUnknownAndTLSBytesStayExact(t *testing.T) {
	for _, wire := range [][]byte{
		[]byte("\r\n\r\nAUDIT unknown protocol\r\n"),
		append([]byte("\r\n"), []byte{0x16, 0x03, 0x01, 0, 5, 1, 0, 0, 1, 0}...),
		[]byte("\r\nINVITE sip:user@audit.invalid SIP/2.0\r\nUser-Agent: AuditUA-SIP\r\n\r\n"),
	} {
		client, server, _ := relayPair(t)
		go func() { _, _ = client.Write(wire) }()
		got := make([]byte, len(wire))
		if _, err := io.ReadFull(server, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, wire) {
			t.Fatal("unknown protocol prefix changed")
		}
	}
}

func TestLeadingCRLFOnPersistentHTTPRequests(t *testing.T) {
	client, server, _ := relayPair(t)
	wire := "\r\nGET / HTTP/1.1\r\nHost: audit.invalid\r\nUser-Agent: AuditUA-Prefix\r\n\r\n"
	go func() { _, _ = io.WriteString(client, wire+wire) }()
	br := bufio.NewReader(server)
	for i := 0; i < 2; i++ {
		req, err := http.ReadRequest(br)
		if err != nil {
			t.Fatal(err)
		}
		if req.UserAgent() != "FFF" {
			t.Fatal("persistent HTTP UA escaped")
		}
	}
}

func TestSlowInitialHTTPHasNoOpaqueTimeoutFallback(t *testing.T) {
	client, server, _ := relayPair(t)
	go func() {
		time.Sleep(2100 * time.Millisecond)
		_, _ = io.WriteString(client, "\r")
		time.Sleep(2100 * time.Millisecond)
		_, _ = io.WriteString(client, "\nG")
		_, _ = io.WriteString(client, "ET / HTTP/1.1\r\nHost: audit.invalid\r\nUser-Agent: AuditUA-Slow\r\n\r\n")
	}()
	req, err := http.ReadRequest(bufio.NewReader(server))
	if err != nil {
		t.Fatal(err)
	}
	if req.UserAgent() != "FFF" {
		t.Fatal("slow HTTP UA escaped")
	}
}

func TestEmptyLinePrefixLimitFailsClosed(t *testing.T) {
	client, server, done := relayPair(t)
	go func() {
		_, _ = io.WriteString(client, strings.Repeat("\r\n", 16*1024)+"GET / HTTP/1.1\r\nHost: audit.invalid\r\nUser-Agent: AuditUA-Prefix\r\n\r\n")
	}()
	got, err := io.ReadAll(server)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("oversized prefix escaped")
	}
	if err = <-done; err == nil {
		t.Fatal("expected bounded prefix rejection")
	}
}
