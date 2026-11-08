package device

import (
	"os"
	"strconv"
	"testing"
)

func loadFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/settings.xml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseUIDumpAll(t *testing.T) {
	nodes, err := ParseUIDump(loadFixture(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 8 {
		t.Fatalf("got %d nodes, want 8", len(nodes))
	}
	for i, n := range nodes {
		if want := "e" + strconv.Itoa(i+1); n.Ref != want {
			t.Errorf("node %d ref = %q, want %q", i, n.Ref, want)
		}
	}
	root := nodes[0]
	if root.Class != "FrameLayout" || root.Depth != 0 || root.Bounds != (Bounds{0, 0, 1080, 2400}) {
		t.Errorf("root = %+v", root)
	}
	wifi := nodes[5]
	if wifi.Text != "Wi‑Fi" || wifi.ID != "title" || wifi.Depth != 3 || !wifi.Clickable || !wifi.LongClickable {
		t.Errorf("wifi = %+v", wifi)
	}
}

func TestParseUIDumpInteractiveOnly(t *testing.T) {
	nodes, err := ParseUIDump(loadFixture(t), true)
	if err != nil {
		t.Fatal(err)
	}
	// Kept: search_bar, its title text, recycler, Wi-Fi, EditText.
	// Dropped: root and row layout (no action/label), zero-size ImageView.
	want := []struct {
		ref, class, id string
		depth          int
	}{
		{"e1", "LinearLayout", "search_bar", 0},
		{"e2", "TextView", "search_action_bar_title", 1},
		{"e3", "RecyclerView", "recycler_view", 0},
		{"e4", "TextView", "title", 1},
		{"e5", "EditText", "input", 1},
	}
	if len(nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d: %+v", len(nodes), len(want), nodes)
	}
	for i, w := range want {
		n := nodes[i]
		if n.Ref != w.ref || n.Class != w.class || n.ID != w.id || n.Depth != w.depth {
			t.Errorf("node %d = {%s %s %s %d}, want %+v", i, n.Ref, n.Class, n.ID, n.Depth, w)
		}
	}
	if !nodes[4].Editable || !nodes[2].Scrollable {
		t.Errorf("flags lost: %+v %+v", nodes[4], nodes[2])
	}
}

func TestParseUIDumpErrors(t *testing.T) {
	if _, err := ParseUIDump([]byte("not xml <"), false); err == nil {
		t.Error("want error for garbage")
	}
	bad := `<hierarchy><node class="a.B" bounds="[1,2]" /></hierarchy>`
	if _, err := ParseUIDump([]byte(bad), false); err == nil {
		t.Error("want error for bad bounds")
	}
}

func TestShortNames(t *testing.T) {
	if got := shortClass("android.widget.Button"); got != "Button" {
		t.Error(got)
	}
	if got := shortClass("View"); got != "View" {
		t.Error(got)
	}
	if got := shortID("com.x:id/ok_btn"); got != "ok_btn" {
		t.Error(got)
	}
	if got := shortID(""); got != "" {
		t.Error(got)
	}
}

func TestDisplayScale(t *testing.T) {
	d := DisplayInfo{Width: 1080, Height: 2400}
	if s := d.Scale(1080); s != 0.45 {
		t.Errorf("scale = %v", s)
	}
	if s := d.Scale(4000); s != 1 {
		t.Errorf("must not upscale, got %v", s)
	}
}

