package ipc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	frames := []struct {
		typ byte
		p   []byte
	}{{TypeJSON, []byte(`{"a":1}`)}, {TypeBinary, []byte{0, 1, 2, 255}}, {TypeJSON, nil}}
	for _, f := range frames {
		if err := WriteFrame(&buf, f.typ, f.p); err != nil {
			t.Fatal(err)
		}
	}
	for i, f := range frames {
		typ, p, err := ReadFrame(&buf)
		if err != nil || typ != f.typ || !bytes.Equal(p, f.p) {
			t.Fatalf("frame %d: %q %v %v", i, typ, p, err)
		}
	}
	if _, _, err := ReadFrame(&buf); err != io.EOF {
		t.Errorf("after last frame: %v, want EOF", err)
	}
}

func TestReadFrameRejects(t *testing.T) {
	hdr := func(n uint32, typ byte) []byte {
		b := make([]byte, 5)
		binary.BigEndian.PutUint32(b, n)
		b[4] = typ
		return b
	}
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"oversized", hdr(MaxFrame+1, TypeJSON), "exceeds limit"},
		{"unknown type", hdr(1, 'X'), "unknown frame type"},
		{"truncated payload", append(hdr(10, TypeJSON), 'a', 'b'), "short frame"},
	}
	for _, tt := range tests {
		if _, _, err := ReadFrame(bytes.NewReader(tt.in)); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, _, err := ReadFrame(bytes.NewReader([]byte{0, 0})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("short header: %v", err)
	}
}

// fakeHelper answers requests on conn using handle, which returns the
// response envelope and an optional binary frame.
func fakeHelper(t *testing.T, conn net.Conn, handle func(req request) (response, []byte)) {
	t.Helper()
	go func() {
		defer conn.Close()
		for {
			_, p, err := ReadFrame(conn)
			if err != nil {
				return
			}
			var req request
			_ = json.Unmarshal(p, &req)
			resp, bin := handle(req)
			out, _ := json.Marshal(resp)
			if WriteFrame(conn, TypeJSON, out) != nil {
				return
			}
			if bin != nil {
				_ = WriteFrame(conn, TypeBinary, bin)
			}
		}
	}()
}

func TestClientCall(t *testing.T) {
	a, b := net.Pipe()
	fakeHelper(t, b, func(req request) (response, []byte) {
		switch req.Op {
		case "echo":
			raw, _ := json.Marshal(req.Args)
			return response{ID: req.ID, OK: true, Result: raw}, nil
		case "shot":
			return response{ID: req.ID, OK: true, Result: json.RawMessage(`{"w":2}`), Binary: true}, []byte{9, 9}
		default:
			return response{ID: req.ID, Error: "unknown op " + req.Op}, nil
		}
	})
	c := NewClient(a)
	defer c.Close()
	ctx := context.Background()

	var echo map[string]int
	if _, err := c.Call(ctx, "echo", map[string]int{"x": 7}, &echo); err != nil || echo["x"] != 7 {
		t.Fatalf("echo = %v, %v", echo, err)
	}
	var shot struct{ W int }
	bin, err := c.Call(ctx, "shot", nil, &shot)
	if err != nil || shot.W != 2 || !bytes.Equal(bin, []byte{9, 9}) {
		t.Fatalf("shot = %+v %v %v", shot, bin, err)
	}
	// A helper-side error keeps the stream usable.
	var remote *RemoteError
	if _, err := c.Call(ctx, "nope", nil, nil); !errors.As(err, &remote) || remote.Msg != "unknown op nope" {
		t.Fatalf("err = %v, want RemoteError", err)
	}
	if _, err := c.Call(ctx, "echo", map[string]int{"x": 1}, &echo); err != nil {
		t.Fatalf("stream broken after remote error: %v", err)
	}
}

func TestClientDetectsHelperDeath(t *testing.T) {
	a, b := net.Pipe()
	go func() {
		_, _, _ = ReadFrame(b)
		b.Close() // dies mid-request
	}()
	c := NewClient(a)
	_, err := c.Call(context.Background(), "screenshot", nil, nil)
	var remote *RemoteError
	if err == nil || errors.As(err, &remote) {
		t.Fatalf("err = %v, want transport error", err)
	}
}

func TestClientRejectsMismatchedID(t *testing.T) {
	a, b := net.Pipe()
	fakeHelper(t, b, func(req request) (response, []byte) { return response{ID: req.ID + 5, OK: true}, nil })
	if _, err := NewClient(a).Call(context.Background(), "x", nil, nil); err == nil || !strings.Contains(err.Error(), "response id") {
		t.Fatalf("err = %v", err)
	}
}
