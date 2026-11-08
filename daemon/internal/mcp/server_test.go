package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/thozoz/droidmcp/daemon/internal/device"
)

// fakeDevice serves testNodes on a 1080x2400 display and records taps.
type fakeDevice struct {
	mu           sync.Mutex
	taps         [][2]int
	displayCalls int
}

func (f *fakeDevice) Screenshot(context.Context, device.ScreenshotOpts) (device.Screenshot, error) {
	return device.Screenshot{Data: []byte{0xFF, 0xD8}, MIME: "image/jpeg", Display: testDisplay}, nil
}
func (f *fakeDevice) UITree(context.Context, bool) ([]device.Node, device.DisplayInfo, error) {
	return testNodes(), testDisplay, nil
}
func (f *fakeDevice) Display(context.Context) (device.DisplayInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.displayCalls++
	return testDisplay, nil
}
func (f *fakeDevice) Tap(_ context.Context, x, y int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.taps = append(f.taps, [2]int{x, y})
	return nil
}
func (f *fakeDevice) LongPress(context.Context, int, int, int) error       { return nil }
func (f *fakeDevice) Swipe(context.Context, int, int, int, int, int) error { return nil }
func (f *fakeDevice) TypeText(context.Context, string) error               { return nil }
func (f *fakeDevice) Key(context.Context, device.Key) error                { return nil }
func (f *fakeDevice) OpenApp(context.Context, string) error                { return nil }
func (f *fakeDevice) ListApps(context.Context) ([]device.App, error)       { return nil, nil }
func (f *fakeDevice) CurrentApp(context.Context) (device.App, error) {
	return device.App{Package: "com.x"}, nil
}
func (f *fakeDevice) Shell(context.Context, string) (string, error) { return "", nil }

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func connect(t *testing.T, url, version string) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	tr := &sdk.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: bearer{"tok"}}}
	var opts *sdk.ClientSessionOptions
	if version != "" {
		opts = &sdk.ClientSessionOptions{ProtocolVersion: version}
	}
	cs, err := client.Connect(context.Background(), tr, opts)
	if err != nil {
		t.Fatalf("connect (%q): %v", version, err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func newTestServer(t *testing.T, dev *fakeDevice) string {
	t.Helper()
	ts := httptest.NewServer(NewHandler("tok", NewServer(dev, Options{MaxEdge: 1080}), nil, nil))
	t.Cleanup(ts.Close)
	return ts.URL + "/mcp"
}

// Both the session-based and the sessionless protocol must reach the tools,
// and refs must be shared between clients because the screen is global.
func TestProtocolsAndSharedState(t *testing.T) {
	dev := &fakeDevice{}
	url := newTestServer(t, dev)

	legacy := connect(t, url, "2025-11-25")
	latest := connect(t, url, "")
	if v := legacy.InitializeResult().ProtocolVersion; v != "2025-11-25" {
		t.Errorf("legacy negotiated %q", v)
	}
	t.Logf("default client negotiated %q", latest.InitializeResult().ProtocolVersion)

	out, isErr := call(t, legacy, "ui_tree", nil)
	if isErr || !strings.Contains(out, "snapshot s1 display=0 rotation=0 size=486x1080 scale=0.45") ||
		!strings.Contains(out, `s1.e1 Button "OK" [45,90,135,180]`) {
		t.Fatalf("ui_tree output:\n%s", out)
	}

	// Ref taken by one client is usable by the other.
	if out, isErr := call(t, latest, "tap", map[string]any{"ref": "s1.e1"}); isErr {
		t.Fatalf("tap ref from other client: %s", out)
	}
	// The tap changed the screen: the ref is now stale for everyone.
	for _, cs := range []*sdk.ClientSession{legacy, latest} {
		if out, isErr := call(t, cs, "tap", map[string]any{"ref": "s1.e1"}); !isErr || !strings.Contains(out, "stale ref, call ui_tree") {
			t.Fatalf("want stale ref, got isErr=%v %q", isErr, out)
		}
	}

	// x,y in screenshot space maps to device px without asking the device.
	before := dev.displayCalls
	if out, isErr := call(t, latest, "tap", map[string]any{"x": 243, "y": 540}); isErr {
		t.Fatalf("tap x,y: %s", out)
	}
	if dev.displayCalls != before {
		t.Errorf("tap x,y called Display %d times", dev.displayCalls-before)
	}
	want := [][2]int{{200, 300}, {540, 1200}} // Button center, then (243,540)/0.45
	if len(dev.taps) != 2 || dev.taps[0] != want[0] || dev.taps[1] != want[1] {
		t.Errorf("taps = %v, want %v", dev.taps, want)
	}
}

func TestShellToolGated(t *testing.T) {
	cs := connect(t, newTestServer(t, &fakeDevice{}), "")
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "shell" {
			t.Fatal("shell tool registered without EnableShell")
		}
	}
}

func TestHandlerRequiresToken(t *testing.T) {
	ts := httptest.NewServer(NewHandler("tok", NewServer(&fakeDevice{}, Options{}), nil, nil))
	defer ts.Close()
	for _, p := range []string{"/mcp", "/health"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without token: %d", p, resp.StatusCode)
		}
	}
}
