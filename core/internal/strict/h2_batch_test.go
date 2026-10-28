package strict

import (
	"bytes"
	"io"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

type recordingSink struct {
	bytes.Buffer
	writes []int
}

func (s *recordingSink) Write(p []byte) (int, error) {
	s.writes = append(s.writes, len(p))
	return s.Buffer.Write(p)
}

func TestH2BufferedControlAndDataBurstByteExact(t *testing.T) {
	var wire bytes.Buffer
	wire.WriteString(preface)
	fr := http2.NewFramer(&wire, nil)
	_ = fr.WriteSettings()
	// Include a batch boundary, an exact 64 KiB frame, an oversized legal
	// extension frame, then small frames again after buffer growth.
	for _, size := range []int{0, 1, 16384, 16384, 16384, 65527, 65528, 70000, 1} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i*31 + size)
		}
		if size <= 16384 {
			_ = fr.WriteData(1, false, data)
		} else {
			_ = fr.WriteRawFrame(http2.FrameType(0xee), 0x29, 3, data)
		}
	}
	for i := 0; i < 8192; i++ {
		_ = fr.WriteWindowUpdate(uint32(i%2), 4096)
	}
	var got recordingSink
	if err := h2(bytes.NewReader(wire.Bytes()), &got, false, "FFF"); err != io.EOF {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), wire.Bytes()) {
		t.Fatal("DATA/extension/control frame bytes or order changed")
	}
	if len(got.writes) >= 100 {
		t.Fatalf("buffered burst not combined: %d writes", len(got.writes))
	}
	for _, n := range got.writes {
		if n > 65536 && n != 65528+9 && n != 70000+9 {
			t.Fatalf("batch exceeds bound: %d bytes", n)
		}
	}
}

func TestH2BufferedInvalidHeaderBarriersNeverLeak(t *testing.T) {
	for _, typ := range []http2.FrameType{http2.FrameHeaders, http2.FrameContinuation, http2.FramePushPromise} {
		t.Run(typ.String(), func(t *testing.T) {
			var valid bytes.Buffer
			valid.WriteString(preface)
			fr := http2.NewFramer(&valid, nil)
			_ = fr.WriteSettings()
			for i := 0; i < 100; i++ {
				_ = fr.WriteWindowUpdate(0, 4096)
			}
			wire := bytes.NewBuffer(append([]byte(nil), valid.Bytes()...))
			fr = http2.NewFramer(wire, nil)
			// Incomplete HPACK, unexpected CONTINUATION, or client PUSH are all
			// invalid. None may be swept into a preceding control-frame batch.
			_ = fr.WriteRawFrame(typ, http2.FlagHeadersEndHeaders, 1, []byte{0xff})
			_ = fr.WritePing(false, [8]byte{1})
			var got bytes.Buffer
			if err := h2(bytes.NewReader(wire.Bytes()), &got, false, "FFF"); err == nil || err == io.EOF {
				t.Fatalf("invalid header boundary accepted: %v", err)
			}
			if !bytes.Equal(got.Bytes(), valid.Bytes()) {
				t.Fatal("invalid header or following frame leaked")
			}
		})
	}
}

func TestH2SmallDataForwardedBeforeNextFrameCompletes(t *testing.T) {
	for _, suffixLength := range []int{0, 5, 11} {
		t.Run(string(rune('a'+suffixLength)), func(t *testing.T) {
			in, send := io.Pipe()
			recv, out := io.Pipe()
			defer in.Close()
			defer send.Close()
			defer recv.Close()
			defer out.Close()
			done := make(chan error, 1)
			go func() { done <- h2(in, out, false, "FFF") }()
			var first, next bytes.Buffer
			first.WriteString(preface)
			_ = http2.NewFramer(&first, nil).WriteData(1, false, []byte("small"))
			_ = http2.NewFramer(&next, nil).WritePing(false, [8]byte{1, 2, 3})
			initial := append(append([]byte(nil), first.Bytes()...), next.Bytes()[:suffixLength]...)
			go func() { _, _ = send.Write(initial) }()
			read := make(chan error, 1)
			go func() {
				got := make([]byte, first.Len())
				_, err := io.ReadFull(recv, got)
				if err == nil && !bytes.Equal(got, first.Bytes()) {
					err = io.ErrUnexpectedEOF
				}
				read <- err
			}()
			select {
			case err := <-read:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("small DATA waited for the absent/partial next frame")
			}
			go func() { _, _ = send.Write(next.Bytes()[suffixLength:]); _ = send.Close() }()
			got := make([]byte, next.Len())
			if _, err := io.ReadFull(recv, got); err != nil || !bytes.Equal(got, next.Bytes()) {
				t.Fatalf("partial next frame changed: %v", err)
			}
			if err := <-done; err != io.EOF {
				t.Fatal(err)
			}
		})
	}
}
