package strict

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

type countingSink struct{ writes int64 }

func (s *countingSink) Write(p []byte) (int, error) { s.writes++; return len(p), nil }

// Isolates frame relay CPU, allocations and write calls. Memory-only results
// are not a substitute for the device's TCP/Clash path measurements.
func BenchmarkH2DataRelay8MiB(b *testing.B) {
	var wire bytes.Buffer
	wire.WriteString(preface)
	fw := http2.NewFramer(&wire, nil)
	_ = fw.WriteSettings()
	var block bytes.Buffer
	enc := hpack.NewEncoder(&block)
	for _, f := range []hpack.HeaderField{{Name: ":method", Value: "POST"}, {Name: ":scheme", Value: "http"}, {Name: ":authority", Value: "bench.test"}, {Name: ":path", Value: "/upload"}, {Name: "user-agent", Value: "AuditUA-Bench"}} {
		_ = enc.WriteField(f)
	}
	_ = fw.WriteHeaders(http2.HeadersFrameParam{StreamID: 1, BlockFragment: block.Bytes(), EndHeaders: true})
	const frameCount = 512
	const payloadSize = 16384
	payload := make([]byte, payloadSize)
	for i := 0; i < frameCount; i++ {
		binary.BigEndian.PutUint32(payload, uint32(i))
		_ = fw.WriteData(1, i == frameCount-1, payload)
	}
	data := wire.Bytes()
	b.SetBytes(frameCount * payloadSize)
	b.ReportAllocs()
	b.ResetTimer()
	var writes int64
	for i := 0; i < b.N; i++ {
		sink := &countingSink{}
		if err := h2(bufio.NewReaderSize(bytes.NewReader(data), 128*1024), sink, false, "FFF"); err != io.EOF {
			b.Fatal(err)
		}
		writes += sink.writes
	}
	b.ReportMetric(float64(writes)/float64(b.N), "writes/op")
}

// Response bodies can generate dense client WINDOW_UPDATE bursts. Keep these
// separate from DATA: both paths must remain exact, without waiting for a batch.
func BenchmarkH2WindowUpdateRelay(b *testing.B) {
	var wire bytes.Buffer
	wire.WriteString(preface)
	fw := http2.NewFramer(&wire, nil)
	_ = fw.WriteSettings()
	for i := 0; i < 8192; i++ {
		_ = fw.WriteWindowUpdate(uint32(i%2), 4096)
	}
	data := wire.Bytes()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	var writes int64
	for i := 0; i < b.N; i++ {
		sink := &countingSink{}
		if err := h2(bufio.NewReaderSize(bytes.NewReader(data), 128*1024), sink, false, "FFF"); err != io.EOF {
			b.Fatal(err)
		}
		writes += sink.writes
	}
	b.ReportMetric(float64(writes)/float64(b.N), "writes/op")
}
