package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// RemoteError is an error the helper reported for one request. The
// connection is still healthy after it.
type RemoteError struct{ Msg string }

func (e *RemoteError) Error() string { return e.Msg }

type request struct {
	ID   uint64 `json:"id"`
	Op   string `json:"op"`
	Args any    `json:"args,omitempty"`
}

type response struct {
	ID     uint64          `json:"id"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Binary bool            `json:"binary,omitempty"`
}

type deadliner interface{ SetDeadline(time.Time) error }

const defaultTimeout = 30 * time.Second

// Client issues one request at a time over a helper connection. Any error
// other than *RemoteError means the stream is unusable and must be closed.
type Client struct {
	mu     sync.Mutex
	conn   io.ReadWriteCloser
	r      *bufio.Reader
	nextID uint64
}

func NewClient(conn io.ReadWriteCloser) *Client {
	return &Client{conn: conn, r: bufio.NewReader(conn)}
}

// Call sends op with args, decodes the result into out (may be nil) and
// returns the trailing binary frame, if any.
func (c *Client) Call(ctx context.Context, op string, args, out any) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d, ok := c.conn.(deadliner); ok {
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(defaultTimeout)
		}
		_ = d.SetDeadline(deadline)
	}

	c.nextID++
	req, err := json.Marshal(request{ID: c.nextID, Op: op, Args: args})
	if err != nil {
		return nil, &RemoteError{Msg: fmt.Sprintf("encode %s: %v", op, err)} // nothing was sent
	}
	if err := WriteFrame(c.conn, TypeJSON, req); err != nil {
		return nil, err
	}
	typ, payload, err := ReadFrame(c.r)
	if err != nil {
		return nil, err
	}
	if typ != TypeJSON {
		return nil, errors.New("ipc: expected JSON response frame")
	}
	var resp response
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("ipc: bad response: %w", err)
	}
	if resp.ID != c.nextID {
		return nil, fmt.Errorf("ipc: response id %d, want %d", resp.ID, c.nextID)
	}
	var bin []byte
	if resp.Binary {
		typ, bin, err = ReadFrame(c.r)
		if err != nil {
			return nil, err
		}
		if typ != TypeBinary {
			return nil, errors.New("ipc: expected binary frame")
		}
	}
	if !resp.OK {
		return nil, &RemoteError{Msg: resp.Error}
	}
	if out != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return nil, &RemoteError{Msg: fmt.Sprintf("decode %s result: %v", op, err)}
		}
	}
	return bin, nil
}

func (c *Client) Close() error { return c.conn.Close() }
