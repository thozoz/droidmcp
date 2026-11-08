package mcp

import (
	"errors"
	"testing"

	"github.com/thozoz/droidmcp/daemon/internal/device"
)

var testDisplay = device.DisplayInfo{Width: 1080, Height: 2400}

func testNodes() []device.Node {
	return []device.Node{
		{Ref: "e1", Class: "Button", Text: "OK", Bounds: device.Bounds{L: 100, T: 200, R: 300, B: 400}},
		{Ref: "e2", Class: "TextView", Text: "Title"},
	}
}

func TestStaleRef(t *testing.T) {
	s := &deviceState{}
	if _, _, err := s.resolve("s1.e1"); !errors.Is(err, errStale) {
		t.Fatalf("no snapshot yet: err = %v, want stale", err)
	}

	id := s.store(s.generation(), testDisplay, testNodes())
	ref := formatRef(id, "e1")
	if ref != "s1.e1" {
		t.Fatalf("ref = %q", ref)
	}
	n, d, err := s.resolve(ref)
	if err != nil || n.Text != "OK" || d != testDisplay {
		t.Fatalf("resolve fresh: %+v %v", n, err)
	}

	s.bump() // a screen-changing action
	if _, _, err := s.resolve(ref); !errors.Is(err, errStale) || err.Error() != "stale ref, call ui_tree" {
		t.Fatalf("after bump: err = %v, want stale", err)
	}
	if n, _, err := s.lookupAny(ref); err != nil || n.Text != "OK" {
		t.Fatalf("lookupAny must ignore staleness: %+v %v", n, err)
	}

	id2 := s.store(s.generation(), testDisplay, testNodes())
	if _, _, err := s.resolve(ref); !errors.Is(err, errStale) {
		t.Errorf("ref of replaced snapshot: err = %v, want stale", err)
	}
	if _, _, err := s.resolve(formatRef(id2, "e2")); err != nil {
		t.Errorf("new snapshot ref: %v", err)
	}
	if _, _, err := s.resolve(formatRef(id2, "e9")); err == nil || errors.Is(err, errStale) {
		t.Errorf("unknown element: err = %v, want not-found", err)
	}
}

func TestDumpRacingAnActionIsStale(t *testing.T) {
	s := &deviceState{}
	gen := s.generation() // dump starts
	s.bump()              // tap lands mid-dump
	id := s.store(gen, testDisplay, testNodes())
	if _, _, err := s.resolve(formatRef(id, "e1")); !errors.Is(err, errStale) {
		t.Errorf("err = %v, want stale", err)
	}
}

func TestGenerationIncrements(t *testing.T) {
	s := &deviceState{}
	for i := range uint64(3) {
		if g := s.generation(); g != i {
			t.Fatalf("generation = %d, want %d", g, i)
		}
		s.bump()
	}
}

func TestParseRef(t *testing.T) {
	for _, bad := range []string{"", "e1", "s1", "s1e1", "x1.e1", "s1.x1", "s.e1", "s1.e", "1.1"} {
		if _, _, err := parseRef(bad); err == nil {
			t.Errorf("parseRef(%q) accepted", bad)
		}
	}
	id, elem, err := parseRef("s12.e345")
	if err != nil || id != 12 || elem != "e345" {
		t.Errorf("got %d %q %v", id, elem, err)
	}
}

func TestScaleRoundTrip(t *testing.T) {
	sp := newSpace(testDisplay, 1080) // scale 0.45
	if sp.scale != 0.45 {
		t.Fatalf("scale = %v", sp.scale)
	}
	// screenshot -> device -> screenshot is exact for every in-range point.
	for x := 0; x < sp.toShot(testDisplay.Width); x += 7 {
		dx, _ := sp.toDevice(x, 0)
		if back := sp.toShot(dx); back != x {
			t.Fatalf("x=%d -> %d -> %d", x, dx, back)
		}
	}
	// device -> screenshot -> device is off by at most one screenshot pixel.
	for dx := 0; dx < testDisplay.Width; dx += 13 {
		got, _ := sp.toDevice(sp.toShot(dx), 0)
		if diff := got - dx; diff < -3 || diff > 3 { // 1/0.45 ≈ 2.2 device px
			t.Fatalf("device %d -> %d", dx, got)
		}
	}
	// Out-of-range input is clamped on screen.
	if x, y := sp.toDevice(-5, 99999); x != 0 || y != testDisplay.Height-1 {
		t.Errorf("clamp = %d,%d", x, y)
	}
	// No downscale when the display already fits.
	if s := newSpace(testDisplay, 4000); s.scale != 1 {
		t.Errorf("scale = %v, want 1", s.scale)
	}
	if h := sp.header(); h != "display=0 rotation=0 size=486x1080 scale=0.45" {
		t.Errorf("header = %q", h)
	}
}
