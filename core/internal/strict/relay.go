// Package strict implements a connection scoped, fail closed HTTP UA filter.
// Once HTTP is recognized no parse error may fall back to a plaintext copy.
package strict

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
)

const preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
const maxHead = 1 << 20

// Relay owns both connections until completion. Responses and bodies are
// streamed; only bounded header blocks are buffered.
func Relay(client, upstream net.Conn) error {
	return RelayWithUA(client, upstream, "FFF")
}

// RelayWithUA applies one immutable UA value throughout a connection,
// including nested HTTP CONNECT tunnels and HTTP/2 stream headers.
func RelayWithUA(client, upstream net.Conn, userAgent string) error {
	return relayWithUA(client, upstream, userAgent, 0)
}

func relayWithUA(client, upstream net.Conn, userAgent string, depth int) error {
	defer client.Close()
	defer upstream.Close()
	if depth > 8 {
		return fmt.Errorf("nested transport limit")
	}
	if !ValidUserAgent(userAgent) {
		return fmt.Errorf("invalid strict User-Agent")
	}
	// Own a single first-byte read per direction, so server-first banners can
	// be forwarded without waiting for a client that is waiting on that banner.
	clientFirst, serverFirst := firstByte(client), firstByte(upstream)
	var clientReader io.Reader
	select {
	case first := <-clientFirst:
		if first.err != nil {
			return first.err
		}
		clientReader = io.MultiReader(bytes.NewReader(first.data), client)
		upstream = &readConn{Conn: upstream, reader: io.MultiReader(&firstReader{result: serverFirst}, upstream)}
	case first := <-serverFirst:
		if first.err != nil {
			return first.err
		}
		reverse := make(chan error, 1)
		go func() { _, e := io.Copy(client, io.MultiReader(bytes.NewReader(first.data), upstream)); reverse <- e }()
		forward := make(chan error, 1)
		go func() {
			br := bufio.NewReaderSize(io.MultiReader(&firstReader{result: clientFirst}, client), 32*1024)
			kind, e := classify(br)
			if e != nil {
				forward <- e
				return
			}
			// HTTP/2 servers may send SETTINGS before the client preface.
			if kind == 2 {
				forward <- h2(br, upstream, false, userAgent)
				return
			}
			if kind == 1 {
				forward <- fmt.Errorf("HTTP/1 request on server-first transport")
				return
			}
			_, e = io.Copy(upstream, br)
			forward <- e
		}()
		return finish(client, upstream, forward, reverse)
	}
	br := bufio.NewReaderSize(clientReader, 32*1024)
	first, e := br.Peek(1)
	if e != nil {
		return e
	}
	if first[0] == 5 {
		if e = socks5(br, client, upstream); e != nil {
			return e
		}
		return relayWithUA(&readConn{Conn: client, reader: br}, upstream, userAgent, depth+1)
	}
	kind, err := classify(br)
	if err != nil {
		return err
	}
	reverse := make(chan error, 1)
	if kind != 1 {
		go func() { _, e := io.Copy(client, upstream); reverse <- e }()
		forward := make(chan error, 1)
		go func() {
			if kind == 2 {
				forward <- h2(br, upstream, false, userAgent)
			} else {
				_, e := io.Copy(upstream, br)
				forward <- e
			}
		}()
		return finish(client, upstream, forward, reverse)
	}
	requests := make(chan exchange, 32)
	stop := make(chan struct{})
	defer close(stop)
	go func() { reverse <- responses(bufio.NewReader(upstream), client, requests) }()
	forward := make(chan error, 1)
	go func() {
		defer close(requests)
		forward <- requestsHTTP(br, client, upstream, requests, stop, userAgent, depth)
	}()
	return finish(client, upstream, forward, reverse)
}

type firstResult struct {
	data []byte
	err  error
}

func firstByte(src io.Reader) <-chan firstResult {
	ch := make(chan firstResult, 1)
	go func() { b := make([]byte, 1); _, err := io.ReadFull(src, b); ch <- firstResult{b, err} }()
	return ch
}

type firstReader struct {
	result   <-chan firstResult
	consumed bool
}

func (r *firstReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.consumed {
		return 0, io.EOF
	}
	r.consumed = true
	first := <-r.result
	if first.err != nil {
		return 0, first.err
	}
	p[0] = first.data[0]
	return 1, nil
}

type readConn struct {
	net.Conn
	reader io.Reader
}

func (c *readConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

// Preserve io.Copy's fast paths through prefix/buffer wrappers. MultiReader
// first drains its owned prefix and then exposes the actual TCP socket; Go's
// Linux TCP ReadFrom can use splice for the remaining opaque stream.
func (c *readConn) WriteTo(dst io.Writer) (int64, error)  { return io.Copy(dst, c.reader) }
func (c *readConn) ReadFrom(src io.Reader) (int64, error) { return io.Copy(c.Conn, src) }
func (c *readConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return nil
}

// ValidUserAgent prevents configuration from injecting a second HTTP field.
func ValidUserAgent(value string) bool {
	if value == "" {
		return false
	}
	for i := range value {
		if value[i] < 32 && value[i] != '\t' || value[i] == 127 {
			return false
		}
	}
	return true
}

func finish(client, upstream net.Conn, forward, reverse <-chan error) error {
	select {
	case err := <-reverse:
		if errors.Is(err, errTunnelHandoff) {
			return <-forward
		}
		return err
	case err := <-forward:
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if c, ok := upstream.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		} else {
			return err
		}
		err = <-reverse
		if errors.Is(err, errTunnelHandoff) {
			return nil
		}
		return err
	}
}

// classify commits only after an HTTP request-line version or the full h2
// preface. SMTP/FTP/SIP text stays opaque. Later HTTP parse errors fail closed.
func classify(br *bufio.Reader) (int, error) {
	methodEnd := false
	lineStart := 0
	for n := 1; n <= 32*1024; n++ {
		p, err := br.Peek(n)
		if err != nil {
			if len(p) > 0 && errors.Is(err, io.EOF) {
				return 0, nil
			}
			return 0, err
		}
		b := p[n-1]
		// RFC 9112 permits HTTP servers to ignore initial empty CRLF lines.
		// Peek past them without consuming: an unknown protocol must retain
		// every original prefix byte. Any initial CR/LF run is probed because
		// tolerant servers also accept bare LF/CR. Non-CRLF prefixes are
		// rejected later by the strict parser rather than copied raw.
		if n == lineStart+1 && (b == '\r' || b == '\n') {
			lineStart = n
			continue
		}
		if !methodEnd {
			if requestSpace(b) {
				methodEnd = true
			} else if !token(b) {
				return 0, nil
			}
		}
		if lineStart == 0 && n == len(preface) && string(p) == preface {
			return 2, nil
		}
		if b == '\n' {
			line := string(p[lineStart:])
			if lineStart == 0 && strings.HasPrefix(line, "PRI * HTTP/2.0\r\n") {
				magic, e := br.Peek(len(preface))
				if e != nil {
					return 0, e
				}
				if string(magic) == preface {
					return 2, nil
				}
				return 1, nil
			}
			// A tolerant backend can accept HTAB/VT/FF/CR separators. These
			// HTTP-shaped lines must reach the HTTP parser, whose rejection is
			// fail closed; they must never select the opaque transport.
			if methodEnd && hasRequestVersion(line) {
				return 1, nil
			}
			return 0, nil
		}
	}
	return 0, fmt.Errorf("request line exceeds limit")
}

func requestSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\v' || b == '\f' || b == '\r'
}

func hasRequestVersion(line string) bool {
	for i := 0; i+1 < len(line); i++ {
		if requestSpace(line[i]) && strings.HasPrefix(line[i+1:], "HTTP/") {
			return true
		}
	}
	return false
}

func token(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(b))
}

// Only called after HTTP classification. Do not apply this to responses or
// chunk trailers, where an empty line has framing semantics of its own.
func readRequestHead(br *bufio.Reader) ([]byte, error) {
	for skipped := 0; ; skipped += 2 {
		first, err := br.Peek(1)
		if err != nil {
			return nil, err
		}
		if first[0] != '\r' {
			break
		}
		prefix, err := br.Peek(2)
		if err != nil {
			return nil, err
		}
		if prefix[1] != '\n' {
			break
		}
		if skipped >= 32*1024 {
			return nil, fmt.Errorf("HTTP empty-line prefix exceeds limit")
		}
		_, _ = br.Discard(2)
	}
	return readHead(br)
}

func readHead(br *bufio.Reader) ([]byte, error) {
	var head []byte
	for {
		line, err := br.ReadSlice('\n')
		head = append(head, line...)
		if len(head) > maxHead {
			return nil, fmt.Errorf("HTTP header exceeds limit")
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(head) >= 4 && bytes.Equal(head[len(head)-4:], []byte("\r\n\r\n")) {
			return head, nil
		}
		if len(line) < 2 || line[len(line)-2] != '\r' {
			return nil, fmt.Errorf("HTTP requires CRLF")
		}
	}
}

func rewriteHead(head []byte, addUA bool, userAgent string) ([]byte, error) {
	lines := bytes.Split(head[:len(head)-2], []byte("\r\n"))
	var out bytes.Buffer
	dropping := false
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		if i == 0 && addUA {
			out.Write(line)
			out.WriteString("\r\n")
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if dropping {
				continue
			}
		} else {
			key, _, ok := bytes.Cut(line, []byte(":"))
			if !ok {
				return nil, fmt.Errorf("invalid HTTP field")
			}
			dropping = strings.EqualFold(string(key), "user-agent")
			if dropping {
				continue
			}
		}
		out.Write(line)
		out.WriteString("\r\n")
	}
	if addUA {
		out.WriteString("User-Agent: " + userAgent + "\r\n")
	}
	out.WriteString("\r\n")
	return out.Bytes(), nil
}

type exchange struct {
	req     *http.Request
	upgrade chan bool
	tunnel  chan io.Reader
}

var errTunnelHandoff = errors.New("HTTP CONNECT tunnel handoff")

func requestsHTTP(br *bufio.Reader, client, dst net.Conn, queue chan<- exchange, stop <-chan struct{}, userAgent string, depth int) error {
	for {
		head, err := readRequestHead(br)
		if err != nil {
			return err
		}
		req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(head)))
		if err != nil {
			return fmt.Errorf("strict request: %w", err)
		}
		if req.Proto != "HTTP/1.0" && req.Proto != "HTTP/1.1" {
			return fmt.Errorf("unsupported HTTP version")
		}
		// Reject ambiguous framing instead of forwarding conflicting fields.
		if len(req.TransferEncoding) > 0 && bytes.Contains(bytes.ToLower(head), []byte("\r\ncontent-length:")) {
			return fmt.Errorf("ambiguous HTTP framing")
		}
		ex := exchange{req: req}
		if req.Header.Get("Upgrade") != "" {
			ex.upgrade = make(chan bool, 1)
		}
		if req.Method == "CONNECT" {
			ex.tunnel = make(chan io.Reader, 1)
			ex.upgrade = nil
		}
		select {
		case queue <- ex:
		case <-stop:
			return net.ErrClosed
		}
		rewritten, err := rewriteHead(head, true, userAgent)
		if err != nil {
			return err
		}
		if _, err = dst.Write(rewritten); err != nil {
			return err
		}
		if err = body(br, dst, req.ContentLength, req.TransferEncoding, true); err != nil {
			return err
		}
		if ex.tunnel != nil {
			select {
			case tunnelReader := <-ex.tunnel:
				if tunnelReader != nil {
					return relayWithUA(&readConn{Conn: client, reader: br}, &readConn{Conn: dst, reader: tunnelReader}, userAgent, depth+1)
				}
			case <-stop:
				return net.ErrClosed
			}
		}
		upgraded := false
		if ex.upgrade != nil {
			select {
			case upgraded = <-ex.upgrade:
			case <-stop:
				return net.ErrClosed
			}
		}
		if upgraded {
			if strings.EqualFold(req.Header.Get("Upgrade"), "h2c") {
				return h2(br, dst, true, userAgent)
			}
			_, err = io.Copy(dst, br)
			return err
		}
	}
}

func responses(br *bufio.Reader, dst io.Writer, queue <-chan exchange) error {
	for ex := range queue {
		for {
			head, err := readHead(br)
			if err != nil {
				return err
			}
			resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(head)), ex.req)
			if err != nil {
				return err
			}
			if _, err = dst.Write(head); err != nil {
				return err
			}
			if resp.StatusCode >= 100 && resp.StatusCode < 200 && resp.StatusCode != 101 {
				continue
			}
			switched := resp.StatusCode == 101 || ex.req.Method == "CONNECT" && resp.StatusCode/100 == 2
			if ex.upgrade != nil {
				ex.upgrade <- switched
			}
			if ex.tunnel != nil {
				if switched {
					ex.tunnel <- br
					return errTunnelHandoff
				}
				ex.tunnel <- nil
			}
			if switched {
				_, err = io.Copy(dst, br)
				return err
			}
			if ex.req.Method != "HEAD" && resp.StatusCode != 204 && resp.StatusCode != 304 {
				if err = body(br, dst, resp.ContentLength, resp.TransferEncoding, false); err != nil {
					return err
				}
			}
			if resp.Close {
				return nil
			}
			break
		}
	}
	return nil
}

func body(br *bufio.Reader, dst io.Writer, length int64, te []string, rewriteTrailers bool) error {
	if len(te) != 0 {
		if len(te) != 1 || te[0] != "chunked" {
			return fmt.Errorf("unsupported transfer encoding")
		}
		for {
			rawLine, err := br.ReadSlice('\n')
			if err != nil {
				return err
			}
			line := string(rawLine)
			if len(line) > 4096 || !strings.HasSuffix(line, "\r\n") {
				return fmt.Errorf("invalid chunk line")
			}
			size := strings.TrimSpace(strings.SplitN(strings.TrimSuffix(line, "\r\n"), ";", 2)[0])
			n, err := strconv.ParseUint(size, 16, 63)
			if err != nil {
				return fmt.Errorf("invalid chunk size")
			}
			if _, err = io.WriteString(dst, line); err != nil {
				return err
			}
			if n == 0 {
				// Trailers cannot introduce a second UA field.
				first, err := br.Peek(2)
				if err != nil {
					return err
				}
				if string(first) == "\r\n" {
					_, _ = br.Discard(2)
					_, err = io.WriteString(dst, "\r\n")
					return err
				}
				tail, err := readHead(br)
				if err != nil {
					return err
				}
				if rewriteTrailers {
					tail, err = rewriteHead(tail, false, "")
					if err != nil {
						return err
					}
				}
				_, err = dst.Write(tail)
				return err
			}
			if _, err = io.CopyN(dst, br, int64(n)); err != nil {
				return err
			}
			var crlf [2]byte
			if _, err = io.ReadFull(br, crlf[:]); err != nil {
				return err
			}
			if string(crlf[:]) != "\r\n" {
				return fmt.Errorf("invalid chunk terminator")
			}
			if _, err = dst.Write(crlf[:]); err != nil {
				return err
			}
		}
	}
	if length < 0 {
		_, err := io.Copy(dst, br)
		return err
	}
	_, err := io.CopyN(dst, br, length)
	return err
}
