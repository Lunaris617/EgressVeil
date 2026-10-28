package strict

import (
	"bufio"
	"fmt"
	"io"
)

// socks5 forwards handshake/authentication bytes without inspecting or logging
// their contents, then returns for HTTP classification of the CONNECT tunnel.
func socks5(src *bufio.Reader, client io.Writer, upstream io.ReadWriter) error {
	copyN := func(dst io.Writer, src io.Reader, n int) error { _, err := io.CopyN(dst, src, int64(n)); return err }
	var pair [2]byte
	if _, err := io.ReadFull(src, pair[:]); err != nil {
		return err
	}
	if pair[0] != 5 || pair[1] == 0 {
		return fmt.Errorf("invalid SOCKS greeting")
	}
	if _, err := upstream.Write(pair[:]); err != nil {
		return err
	}
	if err := copyN(upstream, src, int(pair[1])); err != nil {
		return err
	}
	if _, err := io.ReadFull(upstream, pair[:]); err != nil {
		return err
	}
	if _, err := client.Write(pair[:]); err != nil {
		return err
	}
	if pair[0] != 5 {
		return fmt.Errorf("invalid SOCKS method response")
	}
	switch pair[1] {
	case 0:
	case 2:
		if _, err := io.ReadFull(src, pair[:]); err != nil {
			return err
		}
		if pair[0] != 1 {
			return fmt.Errorf("unsupported SOCKS authentication")
		}
		if _, err := upstream.Write(pair[:]); err != nil {
			return err
		}
		if err := copyN(upstream, src, int(pair[1])); err != nil {
			return err
		}
		n, err := src.ReadByte()
		if err != nil {
			return err
		}
		if _, err = upstream.Write([]byte{n}); err != nil {
			return err
		}
		if err = copyN(upstream, src, int(n)); err != nil {
			return err
		}
		if _, err = io.ReadFull(upstream, pair[:]); err != nil {
			return err
		}
		if _, err = client.Write(pair[:]); err != nil {
			return err
		}
		if pair[0] != 1 || pair[1] != 0 {
			return fmt.Errorf("SOCKS authentication failed")
		}
	default:
		return fmt.Errorf("unsupported SOCKS authentication method")
	}
	if err := socksAddress(src, upstream, true); err != nil {
		return err
	}
	return socksAddress(upstream, client, false)
}

func socksAddress(src io.Reader, dst io.Writer, request bool) error {
	var hdr [4]byte
	if _, err := io.ReadFull(src, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != 5 || hdr[2] != 0 || request && hdr[1] != 1 {
		return fmt.Errorf("unsupported SOCKS CONNECT")
	}
	n := 0
	switch hdr[3] {
	case 1:
		n = 4
	case 4:
		n = 16
	case 3:
		n = 1
	default:
		return fmt.Errorf("invalid SOCKS address type")
	}
	if _, err := dst.Write(hdr[:]); err != nil {
		return err
	}
	addr := make([]byte, n)
	if _, err := io.ReadFull(src, addr); err != nil {
		return err
	}
	if _, err := dst.Write(addr); err != nil {
		return err
	}
	if hdr[3] == 3 {
		n = int(addr[0])
	} else {
		n = 0
	}
	if _, err := io.CopyN(dst, src, int64(n+2)); err != nil {
		return err
	}
	if !request && hdr[1] != 0 {
		return fmt.Errorf("SOCKS CONNECT failed")
	}
	return nil
}
