package device

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var _ Device = (*Exec)(nil)

// ErrInjectionBlocked is returned when the OS refuses input injection from the
// shell uid (e.g. MIUI without the extra security toggle).
var ErrInjectionBlocked = errors.New("input injection blocked; on MIUI enable 'USB debugging (Security settings)' in developer options")

const commandTimeout = 20 * time.Second

type runFunc func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)

// Exec implements Device with stock Android shell tools. Every command is run
// as an argv vector; nothing goes through a shell except the Shell method.
type Exec struct {
	run      runFunc
	dumpPath string
}

func NewExec() *Exec {
	return &Exec{run: runCommand, dumpPath: "/data/local/tmp/droidmcp_ui.xml"}
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// text runs a command whose output is text and folds stderr into the error.
func (e *Exec) text(ctx context.Context, name string, args ...string) (string, error) {
	out, errOut, err := e.run(ctx, name, args...)
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, firstLine(string(errOut)+string(out)))
	}
	return string(out), nil
}

func (e *Exec) input(ctx context.Context, args ...string) error {
	out, errOut, err := e.run(ctx, "input", args...)
	if injectionBlocked(string(out) + string(errOut)) {
		return ErrInjectionBlocked
	}
	if err != nil {
		return fmt.Errorf("input %s: %w: %s", strings.Join(args, " "), err, firstLine(string(errOut)))
	}
	return nil
}

func (e *Exec) Screenshot(ctx context.Context, opts ScreenshotOpts) (Screenshot, error) {
	out, errOut, err := e.run(ctx, "screencap")
	if err != nil {
		return Screenshot{}, fmt.Errorf("screencap: %w: %s", err, firstLine(string(errOut)))
	}
	img, err := parseRawScreencap(out)
	if err != nil {
		return Screenshot{}, err
	}
	d := DisplayInfo{Width: img.Rect.Dx(), Height: img.Rect.Dy(), Rotation: e.rotation(ctx)}
	data, mime, err := encodeScreenshot(img, d.Scale(opts.MaxEdge), opts.Format, opts.Quality)
	if err != nil {
		return Screenshot{}, err
	}
	s := Screenshot{Data: data, MIME: mime, Display: d}
	if looksBlack(img) {
		s.Warning = "screenshot is entirely black: the foreground window is probably FLAG_SECURE (or the screen is off)"
	}
	return s, nil
}

func (e *Exec) UITree(ctx context.Context, interactiveOnly bool) ([]Node, DisplayInfo, error) {
	if err := e.dump(ctx); err != nil {
		return nil, DisplayInfo{}, err
	}
	data, err := os.ReadFile(e.dumpPath)
	if err != nil {
		return nil, DisplayInfo{}, fmt.Errorf("read ui dump: %w", err)
	}
	nodes, err := ParseUIDump(data, interactiveOnly)
	if err != nil {
		return nil, DisplayInfo{}, err
	}
	d, err := e.Display(ctx)
	return nodes, d, err
}

// dump runs `uiautomator dump`, retrying once when the UI never goes idle
// (animations, video).
func (e *Exec) dump(ctx context.Context) error {
	_ = os.Remove(e.dumpPath) // never parse a previous dump
	for attempt := 0; ; attempt++ {
		out, errOut, err := e.run(ctx, "uiautomator", "dump", e.dumpPath)
		msg := string(out) + string(errOut)
		if strings.Contains(msg, "could not get idle state") && attempt == 0 {
			continue
		}
		if err != nil || strings.Contains(msg, "ERROR") {
			return fmt.Errorf("uiautomator dump failed: %s", firstLine(msg))
		}
		return nil
	}
}

func (e *Exec) Tap(ctx context.Context, x, y int) error {
	return e.input(ctx, "tap", strconv.Itoa(x), strconv.Itoa(y))
}

func (e *Exec) LongPress(ctx context.Context, x, y, ms int) error {
	return e.Swipe(ctx, x, y, x, y, ms)
}

func (e *Exec) Swipe(ctx context.Context, x1, y1, x2, y2, ms int) error {
	return e.input(ctx, "swipe", strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2), strconv.Itoa(ms))
}

func (e *Exec) TypeText(ctx context.Context, text string) error {
	arg, err := inputTextArg(text)
	if err != nil {
		return err
	}
	return e.input(ctx, "text", arg)
}

// inputTextArg encodes text for `input text`, which turns "%s" into a space
// and cannot type non-ASCII.
func inputTextArg(text string) (string, error) {
	if text == "" {
		return "", errors.New("text is empty")
	}
	for _, r := range text {
		// shortcut: exec backend is ASCII-only; Phase 1 uses ACTION_SET_TEXT / clipboard paste for Unicode.
		if r < 0x20 || r > 0x7e {
			return "", fmt.Errorf("character %q cannot be typed by the exec backend (printable ASCII only)", r)
		}
	}
	return strings.ReplaceAll(text, " ", "%s"), nil
}

func (e *Exec) Key(ctx context.Context, k Key) error {
	code, ok := Keycodes[k]
	if !ok {
		return fmt.Errorf("unknown key %q", k)
	}
	return e.input(ctx, "keyevent", code)
}

var packageRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+$`)

func (e *Exec) OpenApp(ctx context.Context, pkgOrLabel string) error {
	pkg := pkgOrLabel
	if !packageRe.MatchString(pkg) {
		// shortcut: matches package-name substrings only; real label lookup comes with the Phase 1 helper.
		out, err := e.text(ctx, "pm", "list", "packages")
		if err != nil {
			return err
		}
		matches := matchPackages(parsePackages(out), pkgOrLabel)
		switch len(matches) {
		case 0:
			return fmt.Errorf("no installed package matches %q", pkgOrLabel)
		case 1:
			pkg = matches[0]
		default:
			return fmt.Errorf("%q is ambiguous: %s", pkgOrLabel, strings.Join(matches, ", "))
		}
	}
	out, err := e.text(ctx, "monkey", "-p", pkg, "-c", "android.intent.category.LAUNCHER", "1")
	if err != nil {
		return err
	}
	if strings.Contains(out, "No activities found") || strings.Contains(out, "monkey aborted") {
		return fmt.Errorf("package %s has no launcher activity or is not installed", pkg)
	}
	return nil
}

func (e *Exec) ListApps(ctx context.Context) ([]App, error) {
	out, err := e.text(ctx, "pm", "list", "packages", "-3")
	if err != nil {
		return nil, err
	}
	var apps []App
	for _, p := range parsePackages(out) {
		apps = append(apps, App{Package: p})
	}
	return apps, nil
}

func (e *Exec) CurrentApp(ctx context.Context) (App, error) {
	if out, err := e.text(ctx, "dumpsys", "activity", "activities"); err == nil {
		if app, ok := parseResumedActivity(out); ok {
			return app, nil
		}
	}
	out, err := e.text(ctx, "dumpsys", "window")
	if err != nil {
		return App{}, err
	}
	if app, ok := parseCurrentFocus(out); ok {
		return app, nil
	}
	return App{}, errors.New("could not determine the foreground app")
}

func (e *Exec) Shell(ctx context.Context, cmd string) (string, error) {
	out, errOut, err := e.run(ctx, "sh", "-c", cmd)
	combined := string(out) + string(errOut)
	if err != nil {
		return combined, fmt.Errorf("shell: %w", err)
	}
	return combined, nil
}

// Display returns the current display size (rotated) and rotation.
func (e *Exec) Display(ctx context.Context) (DisplayInfo, error) {
	out, err := e.text(ctx, "wm", "size")
	if err != nil {
		return DisplayInfo{}, err
	}
	w, h, ok := parseWMSize(out)
	if !ok {
		return DisplayInfo{}, fmt.Errorf("cannot parse wm size output: %s", firstLine(out))
	}
	d := DisplayInfo{Width: w, Height: h, Rotation: e.rotation(ctx)}
	if d.Rotation%2 == 1 {
		d.Width, d.Height = h, w
	}
	return d, nil
}

// rotation returns quarter turns; 0 when it cannot be determined.
func (e *Exec) rotation(ctx context.Context) int {
	if out, err := e.text(ctx, "dumpsys", "input"); err == nil {
		if r, ok := parseRotation(out); ok {
			return r
		}
	}
	if out, err := e.text(ctx, "dumpsys", "window", "displays"); err == nil {
		if r, ok := parseRotation(out); ok {
			return r
		}
	}
	return 0
}

var (
	wmPhysicalRe = regexp.MustCompile(`Physical size:\s*(\d+)x(\d+)`)
	wmOverrideRe = regexp.MustCompile(`Override size:\s*(\d+)x(\d+)`)
	rotationRe   = regexp.MustCompile(`(?:SurfaceOrientation:\s*|mCurrentRotation=(?:ROTATION_)?)(\d+)`)
	resumedRe    = regexp.MustCompile(`(?:topResumedActivity|mResumedActivity)[=:]\s*ActivityRecord\{\S+ \S+ ([^/\s}]+)/([^\s}]+)`)
	focusRe      = regexp.MustCompile(`mCurrentFocus=Window\{\S+ \S+ ([^/\s}]+)(?:/([^\s}]+))?\}`)
)

func parseWMSize(out string) (w, h int, ok bool) {
	m := wmOverrideRe.FindStringSubmatch(out)
	if m == nil {
		m = wmPhysicalRe.FindStringSubmatch(out)
	}
	if m == nil {
		return 0, 0, false
	}
	w, _ = strconv.Atoi(m[1])
	h, _ = strconv.Atoi(m[2])
	return w, h, true
}

func parseRotation(out string) (int, bool) {
	m := rotationRe.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	r, _ := strconv.Atoi(m[1])
	if r >= 90 { // ROTATION_90 style
		r /= 90
	}
	return r % 4, true
}

func parseResumedActivity(out string) (App, bool) {
	m := resumedRe.FindStringSubmatch(out)
	if m == nil {
		return App{}, false
	}
	return App{Package: m[1], Activity: m[2]}, true
}

func parseCurrentFocus(out string) (App, bool) {
	m := focusRe.FindStringSubmatch(out)
	if m == nil {
		return App{}, false
	}
	return App{Package: m[1], Activity: m[2]}, true
}

func parsePackages(out string) []string {
	var pkgs []string
	for line := range strings.Lines(out) {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "package:"); ok && p != "" {
			pkgs = append(pkgs, p)
		}
	}
	slices.Sort(pkgs)
	return pkgs
}

func matchPackages(pkgs []string, query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []string
	for _, p := range pkgs {
		if strings.Contains(strings.ToLower(p), q) {
			out = append(out, p)
		}
	}
	return out
}

func injectionBlocked(out string) bool {
	return strings.Contains(out, "SecurityException") || strings.Contains(out, "INJECT_EVENTS")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
