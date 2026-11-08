// Command droidmcpd is the on-device MCP server. Phase 0 runs it from
// /data/local/tmp as the shell user and drives the phone with stock tools.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/thozoz/droidmcp/daemon/internal/auth"
	"github.com/thozoz/droidmcp/daemon/internal/device"
	"github.com/thozoz/droidmcp/daemon/internal/mcp"
)

var version = "dev" // set with -ldflags "-X main.version=..."

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "droidmcpd:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:8765", "listen address; keep 127.0.0.1 unless LAN access is intended")
	token := flag.String("token", "", "bearer token (random when empty)")
	tokenFile := flag.String("token-file", "/data/local/tmp/droidmcp.token", "file the token is written to (0600)")
	maxEdge := flag.Int("max-edge", 1080, "long edge of screenshot space in px; all tool coordinates use this space")
	enableShell := flag.Bool("enable-shell", false, "expose the shell tool")
	apk := flag.String("apk", "", "path to the droidmcp APK; enables the UiAutomation helper backend (otherwise shell tools)")
	flag.Parse()

	// Must survive the adb session ending (USB unplug, wireless debugging off).
	signal.Ignore(syscall.SIGHUP)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if *token == "" {
		t, err := auth.NewToken()
		if err != nil {
			return err
		}
		*token = t
	}
	if *tokenFile != "" {
		_ = os.Remove(*tokenFile) // WriteFile keeps an existing file's mode
		if err := os.WriteFile(*tokenFile, []byte(*token+"\n"), 0o600); err != nil {
			return fmt.Errorf("write token: %w", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var dev device.Device = device.NewExec()
	if *apk != "" {
		h := device.NewHelper(*apk)
		defer h.Close()
		dev = h
	}
	srv := mcp.NewServer(dev, mcp.Options{
		MaxEdge: *maxEdge, EnableShell: *enableShell, Version: version, Logger: logger,
	})
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           mcp.NewHandler(*token, srv, stop, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ListenAndServe() }()
	logger.Info("listening", "addr", *addr, "version", version, "token_file", *tokenFile, "shell", *enableShell, "helper", *apk != "")

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(sctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
