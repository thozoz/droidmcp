package main

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// appLink keeps the companion app informed so it can show the persistent
// notification with the Stop (kill switch) action. Everything is best-effort:
// the daemon works without the app.
type appLink struct {
	pkg    string // e.g. com.efe.droidmcp; empty disables the link
	port   string
	token  string
	logger *slog.Logger
}

func (a appLink) run(name string, args ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, name, args...).CombinedOutput(); err != nil {
		a.logger.Warn("app link command failed", "cmd", name+" "+args[0], "err", err, "out", strings.TrimSpace(string(out))) // args may hold the token
	}
}

func (a appLink) broadcast(action string, extra ...string) {
	args := append([]string{"broadcast", "-a", action, "-n", a.pkg + "/.app.StatusReceiver"}, extra...)
	a.run("am", args...)
}

func (a appLink) started() {
	if a.pkg == "" {
		return
	}
	// The app has no UI to ask for these yet; the shell user may grant them.
	a.run("pm", "grant", a.pkg, "android.permission.POST_NOTIFICATIONS")
	a.run("cmd", "deviceidle", "whitelist", "+"+a.pkg)
	// Explicit component + the receiver's DUMP permission: only the shell can send
	// this and only our app receives it; the app keeps the token for its Stop action.
	a.broadcast("com.efe.droidmcp.DAEMON_STARTED", "--ei", "port", a.port, "--es", "token", a.token)
}

func (a appLink) stopped() {
	if a.pkg != "" {
		a.broadcast("com.efe.droidmcp.DAEMON_STOPPED")
	}
}
