// Package mcp exposes a device.Device as an MCP server over Streamable HTTP.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/thozoz/droidmcp/daemon/internal/auth"
	"github.com/thozoz/droidmcp/daemon/internal/clients"
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

type HandlerOptions struct {
	Token    *auth.Token
	Shutdown func()             // POST /shutdown
	Rotated  func(token string) // after POST /token, to persist the new token
	Clients  *clients.Tracker   // optional; enables GET /clients
	Logger   *slog.Logger
}

// NewHandler serves /mcp, /health, /shutdown, /token and /clients, all behind
// the bearer token. The MCP handler is stateless: no per-session state exists,
// so both session-based (2025-11-25) and sessionless (2026-07-28) clients work.
func NewHandler(srv *sdk.Server, o HandlerOptions) http.Handler {
	token, shutdown, rotated := o.Token, o.Shutdown, o.Rotated
	mux := http.NewServeMux()
	var mcpH http.Handler = sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return srv },
		&sdk.StreamableHTTPOptions{Stateless: true, Logger: o.Logger},
	)
	if o.Clients != nil {
		// Only MCP traffic counts: the app's own /health, /clients polling must not.
		mcpH = o.Clients.Middleware(mcpH)
		mux.HandleFunc("GET /clients", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(o.Clients.Active())
		})
	}
	mux.Handle("/mcp", mcpH)
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
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		t, err := auth.NewToken()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		token.Set(t)
		if rotated != nil {
			rotated(t)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"token":%q}`, t)
	})
	return auth.Middleware(token, mux) // tracker sits behind auth: only token holders count
}
