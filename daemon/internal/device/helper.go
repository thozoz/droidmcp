package device

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/thozoz/droidmcp/daemon/internal/ipc"
)

const (
	maxRestarts   = 3
	restartWindow = time.Minute
	acceptTimeout = 15 * time.Second
)

// spawnFunc starts a helper and returns its connection and a stop func.
type spawnFunc func(ctx context.Context) (io.ReadWriteCloser, func(), error)

// Helper implements Device on top of the Kotlin UiAutomation helper. App and
// package queries have no UiAutomation equivalent and stay on Exec.
//
// Crash policy: a request whose connection dies returns an error and is not
// retried (it may have already acted). The next request starts a fresh
// helper; after maxRestarts within restartWindow it stops trying until the
// window has passed.
type Helper struct {
	*Exec
	spawn spawnFunc

	mu       sync.Mutex
	client   *ipc.Client
	stop     func()
	restarts []time.Time
	now      func() time.Time
}

var (
	_ Device    = (*Helper)(nil)
	_ NodeActor = (*Helper)(nil)
)

// NewHelper runs the helper with app_process from apk, which is either a
// path or a package name. A package is resolved with `pm path` on every
// start, so updating the app does not leave the daemon with a stale path.
func NewHelper(apk string) *Helper {
	e := NewExec()
	return &Helper{Exec: e, spawn: appProcessSpawner(e, apk), now: time.Now}
}

func appProcessSpawner(e *Exec, apk string) spawnFunc {
	return func(ctx context.Context) (io.ReadWriteCloser, func(), error) {
		path := apk
		if !strings.HasPrefix(apk, "/") {
			out, err := e.text(ctx, "pm", "path", apk)
			if err != nil {
				return nil, nil, err
			}
			p, ok := baseAPK(out)
			if !ok {
				return nil, nil, fmt.Errorf("package %s is not installed", apk)
			}
			path = p
		}
		var b [8]byte
		_, _ = rand.Read(b[:])
		name := "droidmcp-" + hex.EncodeToString(b[:])
		l, err := ipc.ListenAbstract(name)
		if err != nil {
			return nil, nil, err
		}
		defer l.Close()

		cmd := exec.Command("app_process", "-Djava.class.path="+path, "/system/bin",
			"com.efe.droidmcp.helper.Main", "serve", name)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr // helper logs land in the daemon log
		if err := cmd.Start(); err != nil {
			return nil, nil, fmt.Errorf("start helper: %w", err)
		}
		stop := func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }

		_ = l.SetDeadline(time.Now().Add(acceptTimeout))
		conn, err := l.AcceptUnix()
		if err != nil {
			stop()
			return nil, nil, fmt.Errorf("helper did not connect: %w", err)
		}
		// The socket name is guessable from /proc/net/unix; only our own child may talk to us.
		uid, pid, err := ipc.PeerCred(conn)
		if err != nil || uid != ipc.ShellUID || int(pid) != cmd.Process.Pid {
			conn.Close()
			stop()
			return nil, nil, fmt.Errorf("rejected helper peer uid=%d pid=%d (want uid %d pid %d): %v",
				uid, pid, ipc.ShellUID, cmd.Process.Pid, err)
		}
		return conn, func() { conn.Close(); stop() }, nil
	}
}

// call runs one request, starting the helper if needed.
func (h *Helper) call(ctx context.Context, op string, args, out any) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.client == nil {
		if err := h.startLocked(ctx); err != nil {
			return nil, err
		}
	}
	bin, err := h.client.Call(ctx, op, args, out)
	var remote *ipc.RemoteError
	if err != nil && !errors.As(err, &remote) {
		h.stopLocked()
		return nil, fmt.Errorf("helper connection lost during %s (%v); it restarts on the next call", op, err)
	}
	return bin, err
}

func (h *Helper) startLocked(ctx context.Context) error {
	now := h.now()
	recent := h.restarts[:0]
	for _, t := range h.restarts {
		if now.Sub(t) < restartWindow {
			recent = append(recent, t)
		}
	}
	h.restarts = recent
	if len(h.restarts) >= maxRestarts {
		return fmt.Errorf("helper failed %d times in the last %v; not restarting yet (see daemon log)", maxRestarts, restartWindow)
	}
	h.restarts = append(h.restarts, now)
	conn, stop, err := h.spawn(ctx)
	if err != nil {
		return err
	}
	h.client, h.stop = ipc.NewClient(conn), stop
	return nil
}

func (h *Helper) stopLocked() {
	if h.stop != nil {
		h.stop()
	}
	h.client, h.stop = nil, nil
}

// Close stops the helper.
func (h *Helper) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopLocked()
}

type wireDisplay struct {
	Width    int `json:"width"`
	Height   int `json:"height"`
	Rotation int `json:"rotation"`
}

func (w wireDisplay) info() DisplayInfo {
	return DisplayInfo{Width: w.Width, Height: w.Height, Rotation: w.Rotation}
}

func (h *Helper) Display(ctx context.Context) (DisplayInfo, error) {
	var d wireDisplay
	_, err := h.call(ctx, "display", nil, &d)
	return d.info(), err
}

func (h *Helper) Screenshot(ctx context.Context, opts ScreenshotOpts) (Screenshot, error) {
	var res struct {
		wireDisplay
		MIME  string `json:"mime"`
		Black bool   `json:"black"`
	}
	format := opts.Format
	if format == "" {
		format = "jpeg"
	}
	if format != "jpeg" && format != "webp" {
		return Screenshot{}, fmt.Errorf("unknown format %q (want jpeg or webp)", format)
	}
	data, err := h.call(ctx, "screenshot", map[string]any{
		"max_edge": opts.MaxEdge, "format": format, "quality": opts.Quality,
	}, &res)
	if err != nil {
		return Screenshot{}, err
	}
	s := Screenshot{Data: data, MIME: res.MIME, Display: res.info()}
	if res.Black {
		s.Warning = "screenshot is entirely black: the foreground window is probably FLAG_SECURE (or the screen is off)"
	}
	return s, nil
}

type wireNode struct {
	Ref           string `json:"ref"`
	Depth         int    `json:"depth"`
	Class         string `json:"class"`
	Text          string `json:"text"`
	Desc          string `json:"desc"`
	ID            string `json:"id"`
	Bounds        [4]int `json:"bounds"`
	Clickable     bool   `json:"clickable"`
	LongClickable bool   `json:"long_clickable"`
	Focusable     bool   `json:"focusable"`
	Focused       bool   `json:"focused"`
	Scrollable    bool   `json:"scrollable"`
	Editable      bool   `json:"editable"`
}

func (h *Helper) UITree(ctx context.Context, interactiveOnly bool) ([]Node, DisplayInfo, error) {
	var res struct {
		Display wireDisplay `json:"display"`
		Nodes   []wireNode  `json:"nodes"`
	}
	if _, err := h.call(ctx, "tree", map[string]any{"interactive_only": interactiveOnly}, &res); err != nil {
		return nil, DisplayInfo{}, err
	}
	nodes := make([]Node, len(res.Nodes))
	for i, w := range res.Nodes {
		nodes[i] = Node{
			Ref: w.Ref, Depth: w.Depth, Class: shortClass(w.Class), Text: w.Text, Desc: w.Desc,
			ID: shortID(w.ID), Bounds: Bounds{w.Bounds[0], w.Bounds[1], w.Bounds[2], w.Bounds[3]},
			Clickable: w.Clickable, LongClickable: w.LongClickable, Focusable: w.Focusable,
			Focused: w.Focused, Scrollable: w.Scrollable, Editable: w.Editable,
		}
	}
	return nodes, res.Display.info(), nil
}

func (h *Helper) Tap(ctx context.Context, x, y int) error {
	_, err := h.call(ctx, "tap", map[string]int{"x": x, "y": y}, nil)
	return err
}

func (h *Helper) LongPress(ctx context.Context, x, y, ms int) error {
	return h.Swipe(ctx, x, y, x, y, ms)
}

func (h *Helper) Swipe(ctx context.Context, x1, y1, x2, y2, ms int) error {
	_, err := h.call(ctx, "swipe", map[string]int{"x1": x1, "y1": y1, "x2": x2, "y2": y2, "ms": ms}, nil)
	return err
}

func (h *Helper) TypeText(ctx context.Context, text string) error {
	if text == "" {
		return errors.New("text is empty")
	}
	_, err := h.call(ctx, "type", map[string]string{"text": text}, nil)
	return err
}

func (h *Helper) Key(ctx context.Context, k Key) error {
	code, ok := Keycodes[k]
	if !ok {
		return fmt.Errorf("unknown key %q", k)
	}
	_, err := h.call(ctx, "key", map[string]string{"code": code}, nil)
	return err
}

func (h *Helper) ClickNode(ctx context.Context, ref string) error {
	_, err := h.call(ctx, "click", map[string]string{"ref": ref}, nil)
	return err
}

func (h *Helper) SetNodeText(ctx context.Context, ref, text string) error {
	_, err := h.call(ctx, "set_text", map[string]string{"ref": ref, "text": text}, nil)
	return err
}

// baseAPK picks the base APK from `pm path` output (split APKs add more lines).
func baseAPK(out string) (string, bool) {
	var first string
	for line := range strings.Lines(out) {
		p, ok := strings.CutPrefix(strings.TrimSpace(line), "package:")
		if !ok {
			continue
		}
		if strings.HasSuffix(p, "/base.apk") {
			return p, true
		}
		if first == "" {
			first = p
		}
	}
	return first, first != ""
}
