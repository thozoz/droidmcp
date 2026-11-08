// Package device abstracts the phone being controlled. All coordinates at this
// layer are device pixels in the current orientation; scaling to screenshot
// space happens in the MCP layer.
package device

import (
	"context"
	"fmt"
)

// Device is the backend contract. Phase 0 implements it with shell commands
// (Exec); Phase 1 swaps in a UiAutomation helper without changing the tools.
type Device interface {
	Screenshot(ctx context.Context, opts ScreenshotOpts) (Screenshot, error)
	UITree(ctx context.Context, interactiveOnly bool) ([]Node, DisplayInfo, error)
	Display(ctx context.Context) (DisplayInfo, error)
	Tap(ctx context.Context, x, y int) error
	LongPress(ctx context.Context, x, y, ms int) error
	Swipe(ctx context.Context, x1, y1, x2, y2, ms int) error
	TypeText(ctx context.Context, text string, replace bool) error
	Key(ctx context.Context, k Key) error
	OpenApp(ctx context.Context, pkgOrLabel string) error
	ListApps(ctx context.Context) ([]App, error)
	CurrentApp(ctx context.Context) (App, error)
	Shell(ctx context.Context, cmd string) (string, error)
}

// NodeActor is implemented by backends that can act on ui_tree nodes
// directly (accessibility actions) instead of tapping their bounds.
type NodeActor interface {
	ClickNode(ctx context.Context, ref string) error
	SetNodeText(ctx context.Context, ref, text string, replace bool) error
}

// DisplayInfo describes the display in device pixels, already rotated
// (Width/Height are as currently seen on screen).
type DisplayInfo struct {
	ID       int
	Width    int
	Height   int
	Rotation int // 0..3, quarter turns
}

// Scale returns the factor that maps device pixels to a screenshot whose long
// edge is at most maxEdge. It never upscales.
func (d DisplayInfo) Scale(maxEdge int) float64 {
	long := max(d.Width, d.Height)
	if maxEdge <= 0 || long <= maxEdge {
		return 1
	}
	return float64(maxEdge) / float64(long)
}

type ScreenshotOpts struct {
	MaxEdge int
	Format  string // "jpeg" or "webp"
	Quality int    // 1..100
}

type Screenshot struct {
	Data    []byte
	MIME    string
	Display DisplayInfo
	Warning string // e.g. FLAG_SECURE suspicion; empty when fine
}

type Bounds struct{ L, T, R, B int }

func (b Bounds) Center() (int, int) { return (b.L + b.R) / 2, (b.T + b.B) / 2 }
func (b Bounds) Empty() bool        { return b.R <= b.L || b.B <= b.T }

type Node struct {
	Ref           string
	Depth         int
	Class         string // short name, e.g. "Button"
	Text          string
	Desc          string
	ID            string // resource-id without the "pkg:id/" prefix
	Bounds        Bounds
	Clickable     bool
	LongClickable bool
	Focusable     bool
	Focused       bool
	Scrollable    bool
	Editable      bool
}

// Signature identifies a node across dumps, where refs are not stable.
func (n Node) Signature() string {
	return fmt.Sprintf("%s|%s|%s|%s", n.Class, n.ID, n.Text, n.Desc)
}

type App struct {
	Package  string
	Activity string // empty when unknown
	Label    string // display label from launcher; empty when unknown
}

type Key string

const (
	KeyHome       Key = "home"
	KeyBack       Key = "back"
	KeyRecents    Key = "recents"
	KeyEnter      Key = "enter"
	KeyBackspace  Key = "backspace"
	KeyWake       Key = "wake"
	KeyPower      Key = "power"
	KeyVolumeUp   Key = "volume_up"
	KeyVolumeDown Key = "volume_down"
)

// Keycodes maps each Key to its android.view.KeyEvent name.
var Keycodes = map[Key]string{
	KeyHome:       "KEYCODE_HOME",
	KeyBack:       "KEYCODE_BACK",
	KeyRecents:    "KEYCODE_APP_SWITCH",
	KeyEnter:      "KEYCODE_ENTER",
	KeyBackspace:  "KEYCODE_DEL",
	KeyWake:       "KEYCODE_WAKEUP",
	KeyPower:      "KEYCODE_POWER",
	KeyVolumeUp:   "KEYCODE_VOLUME_UP",
	KeyVolumeDown: "KEYCODE_VOLUME_DOWN",
}
