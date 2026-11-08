// Command droidmcpd is the on-device MCP server. Phase 0 runs it from
// /data/local/tmp as the shell user and drives the phone with stock tools.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/thozoz/droidmcp/daemon/internal/auth"
	"github.com/thozoz/droidmcp/daemon/internal/clients"
	"github.com/thozoz/droidmcp/daemon/internal/device"
	"github.com/thozoz/droidmcp/daemon/internal/mcp"
)

const shutdownDeadline = 3 * time.Second

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
	keepAwake := flag.Bool("keep-awake", true, "keep screen awake while clients are active")
	app := flag.String("app", "", "companion app package to notify (kill-switch notification); empty disables")
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
	// The token file is how the bridge (via adb) finds the current token.
	writeToken := func(t string) {
		if *tokenFile == "" {
			return
		}
		_ = os.Remove(*tokenFile) // WriteFile keeps an existing file's mode
		if err := os.WriteFile(*tokenFile, []byte(t+"\n"), 0o600); err != nil {
			logger.Error("write token", "err", err)
		}
	}
	writeToken(*token)
	// The SDK logs every stateless request at INFO; keep only its warnings.
	sdkLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var dev device.Device = device.NewExec()
	if *apk != "" {
		h := device.NewHelper(*apk)
		defer h.Close()
		dev = h
	}
	// Client count changes go to the app's notification; a single worker sends
	// the latest count so a slow `am broadcast` never blocks a request.
	clientsChanged := make(chan struct{}, 1)
	tracker := clients.New(2*time.Minute, func(int) {
		select {
		case clientsChanged <- struct{}{}:
		default:
		}
	})
	srv := mcp.NewServer(dev, mcp.Options{
		MaxEdge: *maxEdge, EnableShell: *enableShell, Version: version, Logger: sdkLogger,
	})
	httpSrv := &http.Server{
		Addr: *addr,
		Handler: mcp.NewHandler(srv, mcp.HandlerOptions{
			Token: auth.NewTokenValue(*token), Shutdown: stop, Rotated: writeToken, Clients: tracker, Logger: sdkLogger,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	link := appLink{pkg: *app, port: port, token: *token, logger: logger}
	// The user's own "stay awake" developer setting, restored instead of forced
	// off when we no longer need the screen on.
	prevStayOn := "0"
	if out, err := exec.Command("settings", "get", "global", "stay_on_while_plugged_in").Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" && v != "null" {
			prevStayOn = v
		}
	}
	setStayOn := func(on bool) {
		if !*keepAwake {
			return
		}
		if on {
			_ = exec.Command("svc", "power", "stayon", "true").Run()
			_ = exec.Command("input", "keyevent", "224").Run() // WAKEUP
		} else {
			_ = exec.Command("settings", "put", "global", "stay_on_while_plugged_in", prevStayOn).Run()
		}
	}
	defer setStayOn(false)
	setStayOn(true)

	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }()
	link.started()
	defer link.stopped()
	go tracker.Run(ctx)
	go func() {
		for range clientsChanged {
			n := len(tracker.Active())
			link.clients(n)
			setStayOn(n > 0)
		}
	}()
	logger.Info("listening", "addr", *addr, "version", version, "token_file", *tokenFile, "shell", *enableShell, "helper", *apk != "")

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	// The kill switch must win even if a helper call is stuck holding its lock:
	// exit hard once the deadline passes. The helper dies on socket EOF.
	time.AfterFunc(shutdownDeadline, func() {
		logger.Warn("graceful shutdown timed out; exiting")
		setStayOn(false) // os.Exit skips the deferred restore
		os.Exit(1)
	})
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(sctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
