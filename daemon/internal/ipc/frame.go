// Package ipc is the wire protocol between the Go daemon and the Kotlin
// helper. A frame is a 4-byte big-endian payload length, a 1-byte type and
// the payload. Requests and responses are JSON frames; a response that
// carries bytes (a screenshot) is followed by one binary frame.
package ipc

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	TypeJSON   byte = 'J'
	TypeBinary byte = 'B'

	// MaxFrame bounds a single frame; a full-resolution PNG-free JPEG is far smaller.
	MaxFrame = 64 << 20
)

func WriteFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > MaxFrame {
		return fmt.Errorf("ipc: frame of %d bytes exceeds limit", len(payload))
	}
	var hdr [5]byte
	binary.BigEndian.PutUint32(hdr[:4], uint32(len(payload)))
	hdr[4] = typ
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func ReadFrame(r io.Reader) (typ byte, payload []byte, err error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:4])
	if n > MaxFrame {
		return 0, nil, fmt.Errorf("ipc: frame of %d bytes exceeds limit", n)
	}
	typ = hdr[4]
	if typ != TypeJSON && typ != TypeBinary {
		return 0, nil, fmt.Errorf("ipc: unknown frame type %q", typ)
	}
	payload = make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, fmt.Errorf("ipc: short frame: %w", err)
	}
	return typ, payload, nil
}
