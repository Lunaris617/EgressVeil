package strict

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// h2 retains the client's HPACK decoder across streams. The outgoing encoder
// uses literal never-indexed fields and a zero dynamic table: forwarded server
// SETTINGS can never invalidate its state. DATA and control frames are copied
// byte-for-byte, retaining end-stream, flow control and multiplexing.
func h2(src io.Reader, dst io.Writer, upgraded bool, userAgent string) error {
	// A larger reader preserves bursts already delivered by TCP. Only complete
	// non-header frames present in this buffer may share a Write below. Neither
	// peeking nor batching may wait for an additional byte or a timer.
	br := bufio.NewReaderSize(src, 128*1024)
	src = br
	magic := make([]byte, len(preface))
	if _, err := io.ReadFull(src, magic); err != nil {
		return err
	}
	if string(magic) != preface {
		return fmt.Errorf("invalid h2 client preface")
	}
	if _, err := dst.Write(magic); err != nil {
		return err
	}
	decoder := hpack.NewDecoder(4096, nil)
	decoder.SetMaxStringLength(maxHead)
	decoder.SetAllowedMaxDynamicTableSize(maxHead)
	var block []byte
	var start *http2.HeadersFrame
	var highest uint32
	if upgraded {
		highest = 1
	} // Upgrade request already occupies stream 1.
	// Reuse one frame buffer per connection. DATA/control frames need no
	// decoding: retain their exact wire representation and write it once.
	// Headers still pass through the same HPACK/CONTINUATION validation below.
	const batchLimit = 64 * 1024
	wireBuffer := make([]byte, batchLimit)
	for {
		hdr := wireBuffer[:9]
		if _, err := io.ReadFull(src, hdr); err != nil {
			return err
		}
		length := int(hdr[0])<<16 | int(hdr[1])<<8 | int(hdr[2])
		// We transparently preserve SETTINGS_MAX_FRAME_SIZE negotiation, while
		// bounding allocations by the protocol's absolute maximum.
		if len(wireBuffer) < 9+length {
			grown := make([]byte, 9+length)
			copy(grown, hdr)
			wireBuffer = grown
			hdr = wireBuffer[:9]
		}
		wire := wireBuffer[:9+length]
		payload := wire[9:]
		if _, err := io.ReadFull(src, payload); err != nil {
			return err
		}
		typ := http2.FrameType(hdr[3])
		stream := binary.BigEndian.Uint32(hdr[5:]) & 0x7fffffff
		if start != nil && (typ != http2.FrameContinuation || stream != start.StreamID) {
			return fmt.Errorf("interleaved h2 continuation")
		}
		if typ == http2.FrameContinuation && start == nil {
			return fmt.Errorf("unexpected h2 continuation")
		}
		if typ != http2.FrameHeaders && typ != http2.FrameContinuation {
			if typ == http2.FramePushPromise {
				return fmt.Errorf("client PUSH_PROMISE is invalid")
			}
			// A header or continuation is always a barrier: it must pass through
			// the HPACK validation and UA normalization path. Invalid client PUSH
			// is also left for that iteration's explicit rejection. Peek is used
			// only within Buffered(), so small/incomplete frames never delay wire.
			for len(wire) < batchLimit && br.Buffered() >= 9 {
				next, _ := br.Peek(9)
				nextType := http2.FrameType(next[3])
				if nextType == http2.FrameHeaders || nextType == http2.FrameContinuation || nextType == http2.FramePushPromise {
					break
				}
				n := 9 + (int(next[0]) << 16) + (int(next[1]) << 8) + int(next[2])
				if n > br.Buffered() || n > batchLimit-len(wire) {
					break
				}
				next, _ = br.Peek(n)
				end := len(wire) + n
				copy(wireBuffer[len(wire):end], next)
				_, _ = br.Discard(n)
				wire = wireBuffer[:end]
			}
			if n, err := dst.Write(wire); err != nil {
				return err
			} else if n != len(wire) {
				return io.ErrShortWrite
			}
			continue
		}
		fr := http2.NewFramer(io.Discard, bytes.NewReader(wire))
		// A standalone CONTINUATION is parsed below, since Framer correctly expects
		// a preceding HEADERS when checking a sequence.
		fr.AllowIllegalReads = true
		frame, err := fr.ReadFrame()
		if err != nil {
			return err
		}
		ended := false
		switch f := frame.(type) {
		case *http2.HeadersFrame:
			if f.StreamID == 0 || f.StreamID%2 == 0 {
				return fmt.Errorf("invalid request stream")
			}
			// Copy retained fields; Framer data references the input frame buffer.
			copyStart := *f
			start = &copyStart
			block = append(block[:0], f.HeaderBlockFragment()...)
			ended = f.HeadersEnded()
		case *http2.ContinuationFrame:
			block = append(block, f.HeaderBlockFragment()...)
			ended = f.HeadersEnded()
		default:
			return fmt.Errorf("invalid h2 header frame")
		}
		if len(block) > maxHead {
			return fmt.Errorf("h2 encoded headers exceed limit")
		}
		if !ended {
			continue
		}
		// DecodeFull is bounded additionally by emitted decoded size to protect
		// against a small compressed block expanding to an excessive field list.
		fields := make([]hpack.HeaderField, 0, 16)
		decoded := 0
		decoder.SetEmitFunc(func(f hpack.HeaderField) {
			decoded += len(f.Name) + len(f.Value) + 32
			if decoded <= maxHead {
				fields = append(fields, f)
			}
		})
		if _, err = decoder.Write(block); err != nil {
			return err
		}
		if err = decoder.Close(); err != nil {
			return err
		}
		if decoded > maxHead {
			return fmt.Errorf("h2 decoded headers exceed limit")
		}
		initial := start.StreamID > highest
		fields, err = normalize(fields, initial, userAgent)
		if err != nil {
			return err
		}
		if initial {
			highest = start.StreamID
		}
		var encoded bytes.Buffer
		enc := hpack.NewEncoder(&encoded)
		enc.SetMaxDynamicTableSizeLimit(0)
		enc.SetMaxDynamicTableSize(0)
		for _, f := range fields {
			f.Sensitive = true
			if err = enc.WriteField(f); err != nil {
				return err
			}
		}
		out := http2.NewFramer(dst, nil)
		data := encoded.Bytes()
		first := true
		for first || len(data) > 0 {
			n := len(data)
			limit := 16384
			if first && start.HasPriority() {
				limit -= 5
			}
			if n > limit {
				n = limit
			}
			last := n == len(data)
			if first {
				var flags http2.Flags
				if last {
					flags |= http2.FlagHeadersEndHeaders
				}
				if start.StreamEnded() {
					flags |= http2.FlagHeadersEndStream
				}
				payload := data[:n]
				if start.HasPriority() {
					flags |= http2.FlagHeadersPriority
					priority := make([]byte, 5, n+5)
					dep := start.Priority.StreamDep
					if start.Priority.Exclusive {
						dep |= 0x80000000
					}
					binary.BigEndian.PutUint32(priority, dep)
					priority[4] = start.Priority.Weight
					payload = append(priority, payload...)
				}
				err = out.WriteRawFrame(http2.FrameHeaders, flags, start.StreamID, payload)
			} else {
				err = out.WriteContinuation(start.StreamID, last, data[:n])
			}
			if err != nil {
				return err
			}
			data = data[n:]
			first = false
		}
		start = nil
		block = block[:0]
	}
}

func normalize(fields []hpack.HeaderField, initial bool, userAgent string) ([]hpack.HeaderField, error) {
	out := make([]hpack.HeaderField, 0, len(fields)+1)
	pseudo := map[string]bool{}
	regular := false
	for _, f := range fields {
		// Uppercase UA is accepted for normalization; other uppercase h2 names
		// remain protocol errors, as required by HTTP/2.
		ua := strings.EqualFold(f.Name, "user-agent")
		if f.Name == "" || strings.ContainsAny(f.Value, "\r\n\x00") {
			return nil, fmt.Errorf("invalid h2 field")
		}
		if strings.HasPrefix(f.Name, ":") {
			if !initial || regular || pseudo[f.Name] {
				return nil, fmt.Errorf("invalid h2 pseudo header")
			}
			switch f.Name {
			case ":method", ":scheme", ":authority", ":path", ":protocol":
			default:
				return nil, fmt.Errorf("invalid request pseudo header")
			}
			pseudo[f.Name] = true
		} else {
			regular = true
			for i := range f.Name {
				if !token(f.Name[i]) || f.Name[i] >= 'A' && f.Name[i] <= 'Z' && !ua {
					return nil, fmt.Errorf("invalid h2 field name")
				}
			}
			switch f.Name {
			case "connection", "proxy-connection", "keep-alive", "transfer-encoding", "upgrade":
				return nil, fmt.Errorf("connection specific h2 field")
			}
			if f.Name == "te" && f.Value != "trailers" {
				return nil, fmt.Errorf("invalid h2 te")
			}
		}
		if !ua {
			out = append(out, f)
		}
	}
	if initial {
		if !pseudo[":method"] {
			return nil, fmt.Errorf("missing h2 method")
		}
		out = append(out, hpack.HeaderField{Name: "user-agent", Value: userAgent})
	}
	return out, nil
}
