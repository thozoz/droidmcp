package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/thozoz/droidmcp/daemon/internal/auth"
)

// fakeDaemon is a stateless MCP server behind bearer auth whose token can
// be rotated (a daemon restart) and which counts tool executions.
type fakeDaemon struct {
	mu    sync.Mutex
	token string
	taps  atomic.Int32
	srv   *httptest.Server
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	f := &fakeDaemon{token: "t1"}
	s := sdk.NewServer(&sdk.Implementation{Name: "fake", Version: "0"}, nil)
	s.AddTool(&sdk.Tool{Name: "tap", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			f.taps.Add(1)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "ok"}}}, nil
		})
	mcpH := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, &sdk.StreamableHTTPOptions{Stateless: true})
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		tok := f.token
		f.mu.Unlock()
		auth.Middleware(auth.NewTokenValue(tok), mcpH).ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDaemon) rotate(tok string) {
	f.mu.Lock()
	f.token = tok
	f.mu.Unlock()
}

func newUpstream(f *fakeDaemon, url string) (*upstream, *atomic.Int32, *atomic.Int32) {
	var fetches, prepares atomic.Int32
	return &upstream{
		url: url,
		token: func(context.Context) (string, error) {
			fetches.Add(1)
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.token, nil
		},
		prepare: func(context.Context) error { prepares.Add(1); return nil },
	}, &fetches, &prepares
}

func TestBridgeRefetchesTokenAfterDaemonRestart(t *testing.T) {
	f := newFakeDaemon(t)
	up, fetches, _ := newUpstream(f, f.srv.URL)
	ctx := context.Background()

	if _, err := up.call(ctx, "tap", nil, false); err != nil {
		t.Fatal(err)
	}
	f.rotate("t2") // daemon restarted; the old token is now rejected before any tool runs
	if _, err := up.call(ctx, "tap", nil, false); err != nil {
		t.Fatalf("call after token rotation: %v", err)
	}
	if fetches.Load() != 2 {
		t.Errorf("token fetched %d times, want 2", fetches.Load())
	}
	if f.taps.Load() != 2 {
		t.Errorf("tap ran %d times, want 2", f.taps.Load())
	}
}

func TestBridgeRetriesOnlyWhenSafe(t *testing.T) {
	f := newFakeDaemon(t)
	// A closed port: every dial is refused, so nothing can have run.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := "http://" + l.Addr().String() + "/mcp"
	l.Close()
	up, _, prepares := newUpstream(f, dead)

	_, err := up.call(context.Background(), "tap", nil, false)
	if err == nil || !strings.Contains(err.Error(), "phone unreachable") {
		t.Fatalf("err = %v", err)
	}
	if prepares.Load() != 2 {
		t.Errorf("prepare ran %d times, want 2 (refused dial is safe to retry)", prepares.Load())
	}
}

func TestNotDelivered(t *testing.T) {
	for msg, want := range map[string]bool{
		`Post "http://127.0.0.1:8765/mcp": dial tcp 127.0.0.1:8765: connectex: No connection could be made because the target machine actively refused it. connection refused`: true,
		"rejected by transport: 401 Unauthorized": true,
		`Post "http://127.0.0.1:8765/mcp": EOF`:   false,
		"context deadline exceeded":               false,
	} {
		if got := notDelivered(errString(msg)); got != want {
			t.Errorf("notDelivered(%q) = %v, want %v", msg, got, want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
