// Package mcp exposes a device.Device as an MCP server over Streamable HTTP.
package mcp

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/thozoz/droidmcp/daemon/internal/auth"
	"github.com/thozoz/droidmcp/daemon/internal/device"
)

type Options struct {
	MaxEdge     int  // long edge of screenshot space, in px
	EnableShell bool // register the shell tool
	Version     string
	Logger      *slog.Logger
}

// NewServer builds the MCP server with every tool registered.
func NewServer(dev device.Device, opts Options) *sdk.Server {
	if opts.MaxEdge <= 0 {
		opts.MaxEdge = 1080
	}
	srv := sdk.NewServer(&sdk.Implementation{Name: "droidmcp", Version: opts.Version}, &sdk.ServerOptions{Logger: opts.Logger})
	t := &tools{dev: dev, maxEdge: opts.MaxEdge, state: &deviceState{}}
	t.primeDisplay(opts.Logger)
	t.register(srv, opts.EnableShell)
	return srv
}

// primeDisplay reads the display once at startup so x,y tools work before the
// first screenshot. Afterwards only screenshot and ui_tree refresh it.
func (t *tools) primeDisplay(logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := t.dev.Display(ctx)
	if err != nil {
		if logger != nil {
			logger.Warn("initial display read failed; x,y tools wait for screenshot/ui_tree", "err", err)
		}
		return
	}
	t.state.setDisplay(d)
}

// NewHandler serves /mcp, /health and /shutdown, all behind the bearer token.
// The MCP handler is stateless: no per-session state exists, so both
// session-based (2025-11-25) and sessionless (2026-07-28) clients work.
func NewHandler(token string, srv *sdk.Server, shutdown func(), logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return srv },
		&sdk.StreamableHTTPOptions{Stateless: true, Logger: logger},
	))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("shutting down\n"))
		if shutdown != nil {
			go shutdown()
		}
	})
	return auth.Middleware(token, mux)
}
