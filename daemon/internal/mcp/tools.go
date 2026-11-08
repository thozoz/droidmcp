package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/thozoz/droidmcp/daemon/internal/device"
	"github.com/thozoz/droidmcp/daemon/internal/ipc"
)

const coordNote = " All coordinates are in screenshot space (the space of screenshot/ui_tree output); the server converts them to device pixels."

type tools struct {
	dev     device.Device
	maxEdge int
	state   *deviceState
}

type (
	screenshotIn struct {
		Format  string `json:"format,omitempty" jsonschema:"jpeg (default) or webp"`
		Quality int    `json:"quality,omitempty" jsonschema:"1-100, default 70"`
	}
	uiTreeIn struct {
		InteractiveOnly *bool `json:"interactive_only,omitempty" jsonschema:"only nodes that can be acted on or carry a label (default true)"`
	}
	pointIn struct {
		Ref        string `json:"ref,omitempty" jsonschema:"node ref from ui_tree, e.g. s3.e12; alternative to x,y"`
		X          *int   `json:"x,omitempty" jsonschema:"x in screenshot space"`
		Y          *int   `json:"y,omitempty" jsonschema:"y in screenshot space"`
		DurationMs int    `json:"duration_ms,omitempty" jsonschema:"long_press only: hold time, default 600"`
	}
	swipeIn struct {
		X1         int `json:"x1"`
		Y1         int `json:"y1"`
		X2         int `json:"x2"`
		Y2         int `json:"y2"`
		DurationMs int `json:"duration_ms,omitempty" jsonschema:"default 300"`
	}
	scrollIn struct {
		Ref       string `json:"ref,omitempty" jsonschema:"scrollable node ref; whole screen when omitted"`
		Direction string `json:"direction" jsonschema:"up, down, left or right: the direction the content moves into view (down = reveal what is below)"`
	}
	typeTextIn struct {
		Text    string `json:"text,omitempty" jsonschema:"text to type or set"`
		Ref     string `json:"ref,omitempty" jsonschema:"field to tap first"`
		Replace bool   `json:"replace,omitempty" jsonschema:"replace existing text instead of appending (empty text clears the field)"`
		Submit  bool   `json:"submit,omitempty" jsonschema:"automatically submit/send after typing (presses send button, IME action or enter)"`
	}
	keyIn struct {
		Key string `json:"key" jsonschema:"home, back, recents, enter, backspace, wake, power, volume_up or volume_down"`
	}
	openAppIn struct {
		Package string `json:"package,omitempty" jsonschema:"package name, e.g. com.android.settings"`
		Label   string `json:"label,omitempty" jsonschema:"app name; alternative to package"`
	}
	waitForIn struct {
		Text      string `json:"text,omitempty" jsonschema:"case-insensitive substring of a node's text or content-desc"`
		Ref       string `json:"ref,omitempty" jsonschema:"wait until the node this ref pointed at is on screen again"`
		TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"default 5000, max 60000"`
	}
	shellIn struct {
		Cmd string `json:"cmd" jsonschema:"command line run with sh -c as the shell user"`
	}
	noIn struct{}
)

func (t *tools) register(s *sdk.Server, enableShell bool) {
	ro := &sdk.ToolAnnotations{ReadOnlyHint: true}
	sdk.AddTool(s, &sdk.Tool{Name: "screenshot", Annotations: ro,
		Description: "Capture the screen. Defines screenshot space: the image is scaled so its long edge is at most the server's max edge." + coordNote}, t.screenshot)
	sdk.AddTool(s, &sdk.Tool{Name: "ui_tree", Annotations: ro,
		Description: "Dump the UI hierarchy. Each node gets a ref s<snapshot>.e<n>; refs go stale after any screen-changing tool. Bounds are [left,top,right,bottom]." + coordNote}, t.uiTree)
	sdk.AddTool(s, &sdk.Tool{Name: "tap", Description: "Tap a node (ref) or a point (x,y)." + coordNote}, t.tap)
	sdk.AddTool(s, &sdk.Tool{Name: "long_press", Description: "Long-press a node (ref) or a point (x,y)." + coordNote}, t.longPress)
	sdk.AddTool(s, &sdk.Tool{Name: "swipe", Description: "Swipe from (x1,y1) to (x2,y2)." + coordNote}, t.swipe)
	sdk.AddTool(s, &sdk.Tool{Name: "scroll", Description: "Scroll a scrollable node, or the whole screen." + coordNote}, t.scroll)
	sdk.AddTool(s, &sdk.Tool{Name: "type_text", Description: "Type text into a field (ref) or the focused field; appends by default, or replaces/clears with replace:true. The exec fallback backend accepts printable ASCII only."}, t.typeText)
	sdk.AddTool(s, &sdk.Tool{Name: "key", Description: "Press a system key."}, t.key)
	sdk.AddTool(s, &sdk.Tool{Name: "open_app", Description: "Launch an app by package name or app label."}, t.openApp)
	sdk.AddTool(s, &sdk.Tool{Name: "list_apps", Annotations: ro, Description: "List launchable apps with their labels and package names."}, t.listApps)
	sdk.AddTool(s, &sdk.Tool{Name: "current_app", Annotations: ro, Description: "Report the foreground package and activity."}, t.currentApp)
	sdk.AddTool(s, &sdk.Tool{Name: "wait_for", Annotations: ro,
		Description: "Poll the UI until a node with the text (or the node an old ref pointed at) appears. Returns a fresh ref." + coordNote}, t.waitFor)
	if enableShell {
		sdk.AddTool(s, &sdk.Tool{Name: "shell", Description: "Run a shell command as the shell user (uid 2000)."}, t.shell)
	}
}

func text(s string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: s}}}
}

func (t *tools) screenshot(ctx context.Context, _ *sdk.CallToolRequest, in screenshotIn) (*sdk.CallToolResult, any, error) {
	q := in.Quality
	if q == 0 {
		q = 70
	}
	if q < 1 || q > 100 {
		return nil, nil, errors.New("quality must be 1-100")
	}
	shot, err := t.dev.Screenshot(ctx, device.ScreenshotOpts{MaxEdge: t.maxEdge, Format: in.Format, Quality: q})
	if err != nil {
		return nil, nil, err
	}
	t.state.setDisplay(shot.Display)
	info := newSpace(shot.Display, t.maxEdge).header()
	if shot.Warning != "" {
		info += "\nwarning: " + shot.Warning
	}
	return &sdk.CallToolResult{Content: []sdk.Content{
		&sdk.ImageContent{Data: shot.Data, MIMEType: shot.MIME},
		&sdk.TextContent{Text: info},
	}}, nil, nil
}

func (t *tools) uiTree(ctx context.Context, _ *sdk.CallToolRequest, in uiTreeIn) (*sdk.CallToolResult, any, error) {
	interactive := in.InteractiveOnly == nil || *in.InteractiveOnly
	id, nodes, d, err := t.snapshot(ctx, interactive)
	if err != nil {
		return nil, nil, err
	}
	sp := newSpace(d, t.maxEdge)
	var b strings.Builder
	fmt.Fprintf(&b, "snapshot s%d %s\n", id, sp.header())
	for _, n := range nodes {
		b.WriteString(strings.Repeat("  ", n.Depth))
		b.WriteString(formatNode(id, n, sp))
		b.WriteByte('\n')
	}
	return text(b.String()), nil, nil
}

// snapshot dumps the UI and stores it as the current snapshot.
func (t *tools) snapshot(ctx context.Context, interactive bool) (int, []device.Node, device.DisplayInfo, error) {
	gen := t.state.generation()
	nodes, d, err := t.dev.UITree(ctx, interactive)
	if err != nil {
		return 0, nil, d, err
	}
	return t.state.store(gen, d, nodes), nodes, d, nil
}

func formatNode(id int, n device.Node, sp space) string {
	var b strings.Builder
	b.WriteString(formatRef(id, n.Ref) + " " + n.Class)
	if n.Text != "" {
		fmt.Fprintf(&b, " %q", n.Text)
	}
	if n.Desc != "" {
		fmt.Fprintf(&b, " desc=%q", n.Desc)
	}
	if n.ID != "" {
		b.WriteString(" #" + n.ID)
	}
	bb := sp.bounds(n.Bounds)
	fmt.Fprintf(&b, " [%d,%d,%d,%d]", bb.L, bb.T, bb.R, bb.B)
	for _, f := range []struct {
		on   bool
		name string
	}{{n.Clickable, "clickable"}, {n.LongClickable, "long-clickable"}, {n.Scrollable, "scrollable"}, {n.Editable, "editable"}, {n.Focused, "focused"}} {
		if f.on {
			b.WriteString(" " + f.name)
		}
	}
	return b.String()
}

// point resolves ref or x,y to a device-pixel point.
func (t *tools) point(in pointIn) (int, int, error) {
	if in.Ref != "" {
		n, _, err := t.state.resolve(in.Ref)
		if err != nil {
			return 0, 0, err
		}
		x, y := n.Bounds.Center()
		return x, y, nil
	}
	if in.X == nil || in.Y == nil {
		return 0, 0, errors.New("give either ref or both x and y")
	}
	d, err := t.state.display()
	if err != nil {
		return 0, 0, err
	}
	x, y := newSpace(d, t.maxEdge).toDevice(*in.X, *in.Y)
	return x, y, nil
}

// nodeAction tries an accessibility action on a ref'd node. It reports false
// when the caller should fall back to touching the node's bounds: no ref, a
// backend without node actions, or the app declining the action. Transport
// errors are returned, never retried as a touch, since the action may have run.
func (t *tools) nodeAction(in string, eligible bool, act func(device.NodeActor, string) error) (bool, error) {
	actor, ok := t.dev.(device.NodeActor)
	if in == "" || !ok || !eligible {
		return false, nil
	}
	n, _, err := t.state.resolve(in)
	if err != nil {
		return false, nil // the caller's own resolve reports it
	}
	err = act(actor, n.Ref)
	var remote *ipc.RemoteError
	if errors.As(err, &remote) {
		return false, nil
	}
	return err == nil, err
}

func (t *tools) tap(ctx context.Context, _ *sdk.CallToolRequest, in pointIn) (*sdk.CallToolResult, any, error) {
	clickable := false
	if in.Ref != "" {
		n, _, err := t.state.resolve(in.Ref)
		if err != nil {
			return nil, nil, err
		}
		clickable = n.Clickable
	}
	done, err := t.nodeAction(in.Ref, clickable, func(a device.NodeActor, ref string) error { return a.ClickNode(ctx, ref) })
	if done || err != nil {
		t.state.bump()
		if err != nil {
			return nil, nil, err
		}
		return text("ok (accessibility click)"), nil, nil
	}
	x, y, err := t.point(in)
	if err != nil {
		return nil, nil, err
	}
	defer t.state.bump()
	if err := t.dev.Tap(ctx, x, y); err != nil {
		return nil, nil, err
	}
	return text("ok"), nil, nil
}

func (t *tools) longPress(ctx context.Context, _ *sdk.CallToolRequest, in pointIn) (*sdk.CallToolResult, any, error) {
	x, y, err := t.point(in)
	if err != nil {
		return nil, nil, err
	}
	ms := in.DurationMs
	if ms <= 0 {
		ms = 600
	}
	defer t.state.bump()
	if err := t.dev.LongPress(ctx, x, y, ms); err != nil {
		return nil, nil, err
	}
	return text("ok"), nil, nil
}

func (t *tools) swipe(ctx context.Context, _ *sdk.CallToolRequest, in swipeIn) (*sdk.CallToolResult, any, error) {
	d, err := t.state.display()
	if err != nil {
		return nil, nil, err
	}
	sp := newSpace(d, t.maxEdge)
	x1, y1 := sp.toDevice(in.X1, in.Y1)
	x2, y2 := sp.toDevice(in.X2, in.Y2)
	ms := in.DurationMs
	if ms <= 0 {
		ms = 300
	}
	defer t.state.bump()
	if err := t.dev.Swipe(ctx, x1, y1, x2, y2, ms); err != nil {
		return nil, nil, err
	}
	return text("ok"), nil, nil
}

func (t *tools) scroll(ctx context.Context, _ *sdk.CallToolRequest, in scrollIn) (*sdk.CallToolResult, any, error) {
	var b device.Bounds
	if in.Ref != "" {
		n, _, err := t.state.resolve(in.Ref)
		if err != nil {
			return nil, nil, err
		}
		b = n.Bounds
	} else {
		d, err := t.state.display()
		if err != nil {
			return nil, nil, err
		}
		b = device.Bounds{R: d.Width, B: d.Height}
	}
	x1, y1, x2, y2, err := scrollVector(b, in.Direction)
	if err != nil {
		return nil, nil, err
	}
	defer t.state.bump()
	if err := t.dev.Swipe(ctx, x1, y1, x2, y2, 400); err != nil {
		return nil, nil, err
	}
	return text("ok"), nil, nil
}

// scrollVector returns a swipe over 70% of b; the finger moves opposite to
// the direction the content should reveal.
func scrollVector(b device.Bounds, dir string) (x1, y1, x2, y2 int, err error) {
	cx, cy := b.Center()
	hw, hh := (b.R-b.L)*35/100, (b.B-b.T)*35/100
	switch dir {
	case "down":
		return cx, cy + hh, cx, cy - hh, nil
	case "up":
		return cx, cy - hh, cx, cy + hh, nil
	case "right":
		return cx + hw, cy, cx - hw, cy, nil
	case "left":
		return cx - hw, cy, cx + hw, cy, nil
	}
	return 0, 0, 0, 0, fmt.Errorf("direction must be up, down, left or right, got %q", dir)
}

func (t *tools) typeText(ctx context.Context, _ *sdk.CallToolRequest, in typeTextIn) (*sdk.CallToolResult, any, error) {
	done, err := t.nodeAction(in.Ref, true, func(a device.NodeActor, ref string) error { return a.SetNodeText(ctx, ref, in.Text, in.Replace, in.Submit) })
	if done || err != nil {
		t.state.bump()
		if err != nil {
			return nil, nil, err
		}
		return text("ok"), nil, nil
	}
	if in.Ref != "" {
		n, _, err := t.state.resolve(in.Ref)
		if err != nil {
			return nil, nil, err
		}
		defer t.state.bump()
		x, y := n.Bounds.Center()
		if err := t.dev.Tap(ctx, x, y); err != nil {
			return nil, nil, err
		}
	} else {
		defer t.state.bump()
	}
	if err := t.dev.TypeText(ctx, in.Text, in.Replace, in.Submit); err != nil {
		return nil, nil, err
	}
	return text("ok"), nil, nil
}

func (t *tools) key(ctx context.Context, _ *sdk.CallToolRequest, in keyIn) (*sdk.CallToolResult, any, error) {
	defer t.state.bump()
	if err := t.dev.Key(ctx, device.Key(in.Key)); err != nil {
		return nil, nil, err
	}
	return text("ok"), nil, nil
}

func (t *tools) openApp(ctx context.Context, _ *sdk.CallToolRequest, in openAppIn) (*sdk.CallToolResult, any, error) {
	target := in.Package
	if target == "" {
		target = in.Label
	}
	if target == "" {
		return nil, nil, errors.New("give package or label")
	}
	defer t.state.bump()
	if err := t.dev.OpenApp(ctx, target); err != nil {
		return nil, nil, err
	}
	return text("ok"), nil, nil
}

func (t *tools) listApps(ctx context.Context, _ *sdk.CallToolRequest, _ noIn) (*sdk.CallToolResult, any, error) {
	apps, err := t.dev.ListApps(ctx)
	if err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	for _, a := range apps {
		if a.Label != "" {
			b.WriteString(fmt.Sprintf("%s (%s)\n", a.Label, a.Package))
		} else {
			b.WriteString(a.Package + "\n")
		}
	}
	return text(b.String()), nil, nil
}

func (t *tools) currentApp(ctx context.Context, _ *sdk.CallToolRequest, _ noIn) (*sdk.CallToolResult, any, error) {
	a, err := t.dev.CurrentApp(ctx)
	if err != nil {
		return nil, nil, err
	}
	if a.Activity == "" {
		return text(a.Package), nil, nil
	}
	return text(a.Package + "/" + a.Activity), nil, nil
}

func (t *tools) waitFor(ctx context.Context, _ *sdk.CallToolRequest, in waitForIn) (*sdk.CallToolResult, any, error) {
	var match func(device.Node) bool
	switch {
	case in.Ref != "":
		old, _, err := t.state.lookupAny(in.Ref)
		if err != nil {
			return nil, nil, err
		}
		sig := old.Signature()
		match = func(n device.Node) bool { return n.Signature() == sig }
	case in.Text != "":
		q := strings.ToLower(in.Text)
		match = func(n device.Node) bool {
			return strings.Contains(strings.ToLower(n.Text), q) || strings.Contains(strings.ToLower(n.Desc), q)
		}
	default:
		return nil, nil, errors.New("give text or ref")
	}
	timeout := time.Duration(min(max(in.TimeoutMs, 0), 60000)) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		id, nodes, d, err := t.snapshot(ctx, false)
		if err == nil {
			for _, n := range nodes {
				if match(n) {
					sp := newSpace(d, t.maxEdge)
					return text(fmt.Sprintf("found in snapshot s%d %s\n%s", id, sp.header(), formatNode(id, n, sp))), nil, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			// A dump cut short by our own deadline is not worth reporting.
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				return nil, nil, fmt.Errorf("timed out after %v; last error: %w", timeout, err)
			}
			return nil, nil, fmt.Errorf("timed out after %v", timeout)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (t *tools) shell(ctx context.Context, _ *sdk.CallToolRequest, in shellIn) (*sdk.CallToolResult, any, error) {
	defer t.state.bump()
	out, err := t.dev.Shell(ctx, in.Cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("%w\n%s", err, out)
	}
	return text(out), nil, nil
}
