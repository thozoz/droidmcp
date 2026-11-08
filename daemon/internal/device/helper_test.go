package device

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/thozoz/droidmcp/daemon/internal/ipc"
)

// fakeSpawner starts in-process helpers. behave decides, per spawned helper
// and per request, whether to answer, fail the request or die.
type fakeSpawner struct {
	spawns int
	behave func(spawn int, op string) string // "ok", "remote-error", "die"
}

func (f *fakeSpawner) spawn(context.Context) (io.ReadWriteCloser, func(), error) {
	f.spawns++
	n := f.spawns
	a, b := net.Pipe()
	go func() {
		defer b.Close()
		for {
			_, p, err := ipc.ReadFrame(b)
			if err != nil {
				return
			}
			var req struct {
				ID uint64 `json:"id"`
				Op string `json:"op"`
			}
			_ = json.Unmarshal(p, &req)
			resp := map[string]any{"id": req.ID, "ok": true}
			switch f.behave(n, req.Op) {
			case "die":
				return
			case "remote-error":
				resp = map[string]any{"id": req.ID, "error": "node is gone"}
			}
			out, _ := json.Marshal(resp)
			if ipc.WriteFrame(b, ipc.TypeJSON, out) != nil {
				return
			}
		}
	}()
	return a, func() { a.Close() }, nil
}

func newTestHelper(f *fakeSpawner, now *time.Time) *Helper {
	return &Helper{spawn: f.spawn, now: func() time.Time { return *now }}
}

func TestHelperCrashReturnsErrorThenRestarts(t *testing.T) {
	now := time.Unix(0, 0)
	f := &fakeSpawner{behave: func(spawn int, op string) string {
		if spawn == 1 && op == "tap" {
			return "die"
		}
		return "ok"
	}}
	h := newTestHelper(f, &now)
	ctx := context.Background()

	if err := h.Key(ctx, KeyHome); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// The helper dies while handling tap: the tap is reported, not retried.
	err := h.Tap(ctx, 1, 2)
	if err == nil || !strings.Contains(err.Error(), "helper connection lost during tap") {
		t.Fatalf("tap err = %v", err)
	}
	if f.spawns != 1 {
		t.Fatalf("spawns = %d after crash, want 1 (no automatic retry)", f.spawns)
	}
	// The next call brings up a fresh helper.
	if err := h.Tap(ctx, 1, 2); err != nil {
		t.Fatalf("call after crash: %v", err)
	}
	if f.spawns != 2 {
		t.Errorf("spawns = %d, want 2", f.spawns)
	}
}

func TestHelperRemoteErrorKeepsConnection(t *testing.T) {
	now := time.Unix(0, 0)
	f := &fakeSpawner{behave: func(_ int, op string) string {
		if op == "click" {
			return "remote-error"
		}
		return "ok"
	}}
	h := newTestHelper(f, &now)
	var remote *ipc.RemoteError
	if err := h.ClickNode(context.Background(), "e1"); !errors.As(err, &remote) {
		t.Fatalf("err = %v, want RemoteError", err)
	}
	if err := h.Tap(context.Background(), 0, 0); err != nil || f.spawns != 1 {
		t.Fatalf("err=%v spawns=%d: remote error must not restart the helper", err, f.spawns)
	}
}

func TestHelperRestartLimit(t *testing.T) {
	now := time.Unix(0, 0)
	f := &fakeSpawner{behave: func(int, string) string { return "die" }}
	h := newTestHelper(f, &now)
	ctx := context.Background()

	for i := range maxRestarts {
		if err := h.Tap(ctx, 0, 0); err == nil || !strings.Contains(err.Error(), "connection lost") {
			t.Fatalf("attempt %d: %v", i, err)
		}
		now = now.Add(time.Second)
	}
	err := h.Tap(ctx, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "not restarting yet") {
		t.Fatalf("err = %v, want restart limit", err)
	}
	if f.spawns != maxRestarts {
		t.Fatalf("spawns = %d, want %d", f.spawns, maxRestarts)
	}
	// Once the window passes, it tries again.
	now = now.Add(restartWindow)
	f.behave = func(int, string) string { return "ok" }
	if err := h.Tap(ctx, 0, 0); err != nil {
		t.Fatalf("after window: %v", err)
	}
}

func TestBaseAPK(t *testing.T) {
	split := "package:/data/app/x/split_config.arm64_v8a.apk\npackage:/data/app/x/base.apk\n"
	if p, ok := baseAPK(split); !ok || p != "/data/app/x/base.apk" {
		t.Errorf("split: %q %v", p, ok)
	}
	if p, ok := baseAPK("package:/data/app/y/app.apk\r\n"); !ok || p != "/data/app/y/app.apk" {
		t.Errorf("single: %q %v", p, ok)
	}
	if _, ok := baseAPK(""); ok {
		t.Error("empty output must not resolve")
	}
}
