package mcp

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/thozoz/droidmcp/daemon/internal/device"
)

var errStale = errors.New("stale ref, call ui_tree")

// deviceState is shared by every client: the screen is one global resource,
// so any screen-changing action invalidates refs for everybody.
type deviceState struct {
	mu     sync.Mutex
	gen    uint64 // bumped by every screen-changing tool
	lastID int
	snap   *snapshot
	disp   device.DisplayInfo // last known display; x,y tools read only this
}

func (s *deviceState) setDisplay(d device.DisplayInfo) {
	s.mu.Lock()
	s.disp = d
	s.mu.Unlock()
}

// display returns the cached display without touching the device.
func (s *deviceState) display() (device.DisplayInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disp.Width == 0 || s.disp.Height == 0 {
		return device.DisplayInfo{}, errors.New("display size unknown, call screenshot or ui_tree first")
	}
	return s.disp, nil
}

type snapshot struct {
	id      int
	gen     uint64 // generation the dump was started at
	display device.DisplayInfo
	nodes   map[string]device.Node // "e12" -> node, device px
}

// generation returns the current generation; capture it before dumping so an
// action racing with the dump makes the result stale.
func (s *deviceState) generation() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// bump marks every existing ref stale.
func (s *deviceState) bump() {
	s.mu.Lock()
	s.gen++
	s.mu.Unlock()
}

// store records a new snapshot taken at generation gen and returns its id.
func (s *deviceState) store(gen uint64, d device.DisplayInfo, nodes []device.Node) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastID++
	m := make(map[string]device.Node, len(nodes))
	for _, n := range nodes {
		m[n.Ref] = n
	}
	s.snap = &snapshot{id: s.lastID, gen: gen, display: d, nodes: m}
	s.disp = d
	return s.lastID
}

// resolve returns the node for a "s<id>.e<n>" ref from the current snapshot.
func (s *deviceState) resolve(ref string) (device.Node, device.DisplayInfo, error) {
	return s.lookup(ref, true)
}

// lookupAny is resolve without the freshness check; wait_for uses it to learn
// what an old ref pointed at.
func (s *deviceState) lookupAny(ref string) (device.Node, device.DisplayInfo, error) {
	return s.lookup(ref, false)
}

func (s *deviceState) lookup(ref string, fresh bool) (device.Node, device.DisplayInfo, error) {
	id, elem, err := parseRef(ref)
	if err != nil {
		return device.Node{}, device.DisplayInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snap == nil || s.snap.id != id || (fresh && s.snap.gen != s.gen) {
		return device.Node{}, device.DisplayInfo{}, errStale
	}
	n, ok := s.snap.nodes[elem]
	if !ok {
		return device.Node{}, device.DisplayInfo{}, fmt.Errorf("ref %s not found in snapshot s%d", ref, id)
	}
	return n, s.snap.display, nil
}

// parseRef splits "s3.e12" into 3 and "e12".
func parseRef(ref string) (int, string, error) {
	sPart, ePart, ok := strings.Cut(ref, ".")
	idStr, okS := strings.CutPrefix(sPart, "s")
	nStr, okE := strings.CutPrefix(ePart, "e")
	id, errS := strconv.Atoi(idStr)
	_, errE := strconv.Atoi(nStr)
	if !ok || !okS || !okE || errS != nil || errE != nil {
		return 0, "", fmt.Errorf("bad ref %q: want s<snapshot>.e<n>, e.g. s3.e12", ref)
	}
	return id, ePart, nil
}

func formatRef(id int, elem string) string { return "s" + strconv.Itoa(id) + "." + elem }

// space converts between device pixels and screenshot space, where every
// tool coordinate lives. It is derived from the display alone (stateless).
type space struct {
	scale float64
	d     device.DisplayInfo
}

func newSpace(d device.DisplayInfo, maxEdge int) space { return space{scale: d.Scale(maxEdge), d: d} }

func (s space) toShot(v int) int { return int(math.Round(float64(v) * s.scale)) }

// toDevice maps a screenshot-space point to device px, clamped on screen.
func (s space) toDevice(x, y int) (int, int) {
	dx := int(math.Round(float64(x) / s.scale))
	dy := int(math.Round(float64(y) / s.scale))
	return min(max(dx, 0), s.d.Width-1), min(max(dy, 0), s.d.Height-1)
}

func (s space) bounds(b device.Bounds) device.Bounds {
	return device.Bounds{L: s.toShot(b.L), T: s.toShot(b.T), R: s.toShot(b.R), B: s.toShot(b.B)}
}

// header is the line every screenshot / ui_tree result starts with.
func (s space) header() string {
	return fmt.Sprintf("display=%d rotation=%d size=%dx%d scale=%.4g",
		s.d.ID, s.d.Rotation, s.toShot(s.d.Width), s.toShot(s.d.Height), s.scale)
}
