package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"syscall"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// upstream is the bridge's connection to the daemon on the phone. It
// re-runs prepare (adb wait-for-device + forward) and refetches the token
// whenever the connection breaks, so the MCP client never has to reconnect.
type upstream struct {
	url     string
	token   func(context.Context) (string, error) // reads the daemon's current token
	prepare func(context.Context) error           // makes url reachable (adb forward)

	mu  sync.Mutex
	cs  *sdk.ClientSession
	tok string
}

type bearer struct {
	tok  string
	base http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return b.base.RoundTrip(r)
}

func (u *upstream) session(ctx context.Context) (*sdk.ClientSession, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cs != nil {
		return u.cs, nil
	}
	if err := u.prepare(ctx); err != nil {
		return nil, err
	}
	if u.tok == "" {
		tok, err := u.token(ctx)
		if err != nil {
			return nil, fmt.Errorf("read daemon token: %w", err)
		}
		u.tok = tok
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "droidmcp-bridge", Version: version}, nil)
	cs, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:   u.url,
		HTTPClient: &http.Client{Transport: bearer{u.tok, http.DefaultTransport}},
		MaxRetries: -1, // retries are decided here, with knowledge of the tool
	}, nil)
	if err != nil {
		if isAuth(err) {
			u.tok = "" // the daemon restarted with a new token
		}
		return nil, err
	}
	u.cs = cs
	return cs, nil
}

// reset drops the session; the next call reconnects. dropToken forces a
// token refetch (the daemon restarted).
func (u *upstream) reset(dropToken bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cs != nil {
		_ = u.cs.Close()
		u.cs = nil
	}
	if dropToken {
		u.tok = ""
	}
}

// call forwards one tool call. It retries once when that cannot repeat an
// action: the request never reached the daemon (dial failure, bad token) or
// the tool is read-only. A tap whose response was lost is not repeated.
func (u *upstream) call(ctx context.Context, name string, args json.RawMessage, readOnly bool) (*sdk.CallToolResult, error) {
	var lastErr error
	for range 2 {
		cs, err := u.session(ctx)
		if err == nil {
			var res *sdk.CallToolResult
			res, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
			if err == nil {
				return res, nil
			}
			u.reset(isAuth(err))
			if !readOnly && !notDelivered(err) {
				return nil, fmt.Errorf("lost the phone during %s; it may or may not have run: %w", name, err)
			}
		}
		lastErr = err
	}
	return nil, fmt.Errorf("phone unreachable: %w", lastErr)
}

func (u *upstream) tools(ctx context.Context) ([]*sdk.Tool, error) {
	cs, err := u.session(ctx)
	if err != nil {
		return nil, err
	}
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		u.reset(isAuth(err))
		return nil, err
	}
	return res.Tools, nil
}

func isAuth(err error) bool {
	s := err.Error()
	return strings.Contains(s, "401") || strings.Contains(s, "Unauthorized")
}

// notDelivered reports errors that guarantee the daemon never saw the call.
func notDelivered(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || isAuth(err) ||
		strings.Contains(err.Error(), "connection refused") // Windows reports WSAECONNREFUSED
}
