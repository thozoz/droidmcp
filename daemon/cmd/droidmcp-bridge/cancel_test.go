package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A client cancelling a forwarded call must end the HTTP request to the
// daemon: that is the only cancel signal a stateless daemon can see.
func TestCancelEndsUpstreamRequest(t *testing.T) {
	s := sdk.NewServer(&sdk.Implementation{Name: "fake", Version: "0"}, nil)
	s.AddTool(&sdk.Tool{Name: "wait_for", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			time.Sleep(1500 * time.Millisecond)
			return &sdk.CallToolResult{}, nil
		})
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, &sdk.StreamableHTTPOptions{Stateless: true})
	ended := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		if bytes.Contains(body, []byte(`"tools/call"`)) {
			go func() { <-r.Context().Done(); close(ended) }()
		}
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()
	up := &upstream{url: srv.URL, token: func(context.Context) (string, error) { return "t", nil }, prepare: func(context.Context) error { return nil }}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	_, _ = up.call(ctx, "wait_for", nil, true)
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("upstream request still open after the client cancelled")
	}
}
