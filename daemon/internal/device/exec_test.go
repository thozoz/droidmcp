package device

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInputTextArg(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{"hello", "hello", false},
		{"a b  c", "a%sb%s%sc", false},
		{`it's "quoted"; rm -rf &`, `it's%s"quoted";%srm%s-rf%s&`, false},
		{"", "", true},
		{"çay", "", true},
		{"line\nbreak", "", true},
	}
	for _, tt := range tests {
		got, err := inputTextArg(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("inputTextArg(%q) = %q, %v", tt.in, got, err)
		}
	}
}

func TestInjectionBlocked(t *testing.T) {
	if !injectionBlocked("java.lang.SecurityException: Injecting input events requires the caller (or the source of the instrumentation, if any) to have the INJECT_EVENTS permission.") {
		t.Error("MIUI error not detected")
	}
	if injectionBlocked("") {
		t.Error("empty output flagged")
	}
}

// fakeRun dispatches on the command name and records argv.
type fakeRun struct {
	calls   [][]string
	replies map[string][]reply // name -> queued replies
}

type reply struct {
	out string
	err error
}

func (f *fakeRun) run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	q := f.replies[name]
	if len(q) == 0 {
		return nil, nil, nil
	}
	r := q[0]
	f.replies[name] = q[1:]
	return []byte(r.out), nil, r.err
}

func TestInputUsesArgvAndDetectsBlock(t *testing.T) {
	f := &fakeRun{replies: map[string][]reply{
		"input": {{}, {out: "SecurityException: INJECT_EVENTS", err: errors.New("exit status 1")}},
	}}
	e := &Exec{run: f.run}
	if err := e.TypeText(context.Background(), "a b;c"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"input", "text", "a%sb;c"}; !slices.Equal(f.calls[0], want) {
		t.Errorf("argv = %q, want %q", f.calls[0], want)
	}
	if err := e.Tap(context.Background(), 1, 2); !errors.Is(err, ErrInjectionBlocked) {
		t.Errorf("err = %v, want ErrInjectionBlocked", err)
	}
}

func TestDumpRetriesIdleStateOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ui.xml")
	idle := reply{out: "ERROR: could not get idle state."}
	f := &fakeRun{replies: map[string][]reply{"uiautomator": {idle, {out: "UI hierchary dumped to: " + path}}}}
	e := &Exec{run: f.run, dumpPath: path}
	if err := e.dump(context.Background()); err != nil {
		t.Fatalf("retry should succeed: %v", err)
	}
	if len(f.calls) != 2 {
		t.Errorf("calls = %d, want 2", len(f.calls))
	}

	f = &fakeRun{replies: map[string][]reply{"uiautomator": {idle, idle}}}
	e = &Exec{run: f.run, dumpPath: path}
	if err := e.dump(context.Background()); err == nil || !strings.Contains(err.Error(), "idle state") {
		t.Errorf("err = %v, want idle state error after one retry", err)
	}
	if len(f.calls) != 2 {
		t.Errorf("calls = %d, want 2 (only one retry)", len(f.calls))
	}
}

func TestUITreeReadsDump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ui.xml")
	xml, _ := os.ReadFile("testdata/settings.xml")
	f := &fakeRun{replies: map[string][]reply{
		"wm":      {{out: "Physical size: 1080x2400\n"}},
		"dumpsys": {{out: "  SurfaceOrientation: 1\n"}},
	}}
	e := &Exec{run: func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		if name == "uiautomator" {
			return nil, nil, os.WriteFile(path, xml, 0o600)
		}
		return f.run(ctx, name, args...)
	}, dumpPath: path}
	nodes, d, err := e.UITree(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 5 || d != (DisplayInfo{Width: 2400, Height: 1080, Rotation: 1}) {
		t.Errorf("nodes=%d display=%+v", len(nodes), d)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("dump file left on disk: %v", err)
	}
}

func TestParsers(t *testing.T) {
	if w, h, ok := parseWMSize("Physical size: 1080x2400\nOverride size: 720x1600\n"); !ok || w != 720 || h != 1600 {
		t.Errorf("override not preferred: %d %d", w, h)
	}
	for in, want := range map[string]int{
		"SurfaceOrientation: 3":         3,
		"mCurrentRotation=ROTATION_90":  1,
		"mCurrentRotation=ROTATION_180": 2,
		"mCurrentRotation=0":            0,
	} {
		if r, ok := parseRotation(in); !ok || r != want {
			t.Errorf("parseRotation(%q) = %d, %v", in, r, ok)
		}
	}
	if _, ok := parseRotation("nothing"); ok {
		t.Error("parseRotation should fail")
	}

	act := "  topResumedActivity=ActivityRecord{1a2b u0 com.android.settings/.Settings t42}\n"
	if a, ok := parseResumedActivity(act); !ok || a != (App{"com.android.settings", ".Settings"}) {
		t.Errorf("resumed = %+v %v", a, ok)
	}
	old := "    mResumedActivity: ActivityRecord{1a2b u0 com.whatsapp/.HomeActivity t7}"
	if a, ok := parseResumedActivity(old); !ok || a.Package != "com.whatsapp" {
		t.Errorf("mResumedActivity = %+v %v", a, ok)
	}
	focus := "  mCurrentFocus=Window{9f u0 com.android.chrome/org.chromium.chrome.browser.ChromeTabbedActivity}"
	if a, ok := parseCurrentFocus(focus); !ok || a.Package != "com.android.chrome" {
		t.Errorf("focus = %+v %v", a, ok)
	}
	if a, ok := parseCurrentFocus("mCurrentFocus=Window{9f u0 NotificationShade}"); !ok || a.Package != "NotificationShade" || a.Activity != "" {
		t.Errorf("focus no activity = %+v %v", a, ok)
	}
	if _, ok := parseCurrentFocus("mCurrentFocus=null"); ok {
		t.Error("null focus should not match")
	}

	pkgs := parsePackages("package:com.b\npackage:com.a\n\njunk\n")
	if !slices.Equal(pkgs, []string{"com.a", "com.b"}) {
		t.Errorf("packages = %v", pkgs)
	}
	if m := matchPackages([]string{"com.whatsapp", "com.android.settings"}, "WhatsApp"); !slices.Equal(m, []string{"com.whatsapp"}) {
		t.Errorf("match = %v", m)
	}
}
