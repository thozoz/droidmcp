package device

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

type xmlNode struct {
	Text          string    `xml:"text,attr"`
	ResourceID    string    `xml:"resource-id,attr"`
	Class         string    `xml:"class,attr"`
	Desc          string    `xml:"content-desc,attr"`
	Clickable     bool      `xml:"clickable,attr"`
	LongClickable bool      `xml:"long-clickable,attr"`
	Focusable     bool      `xml:"focusable,attr"`
	Focused       bool      `xml:"focused,attr"`
	Scrollable    bool      `xml:"scrollable,attr"`
	Bounds        string    `xml:"bounds,attr"`
	Children      []xmlNode `xml:"node"`
}

type xmlHierarchy struct {
	Nodes []xmlNode `xml:"node"`
}

// ParseUIDump converts a `uiautomator dump` XML document into a flat list of
// nodes in document order. Refs e1..eN are assigned to emitted nodes in that
// order. With interactiveOnly, nodes that carry no action and no label are
// dropped and Depth counts only emitted ancestors.
func ParseUIDump(data []byte, interactiveOnly bool) ([]Node, error) {
	var h xmlHierarchy
	if err := xml.Unmarshal(data, &h); err != nil {
		return nil, fmt.Errorf("parse ui dump: %w", err)
	}
	var out []Node
	var walk func(x xmlNode, depth int) error
	walk = func(x xmlNode, depth int) error {
		n, err := convertNode(x)
		if err != nil {
			return err
		}
		childDepth := depth
		if !interactiveOnly || n.interesting() {
			n.Depth = depth
			n.Ref = "e" + strconv.Itoa(len(out)+1)
			out = append(out, n)
			childDepth = depth + 1
		}
		for _, c := range x.Children {
			if err := walk(c, childDepth); err != nil {
				return err
			}
		}
		return nil
	}
	for _, x := range h.Nodes {
		if err := walk(x, 0); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func convertNode(x xmlNode) (Node, error) {
	b, err := parseBounds(x.Bounds)
	if err != nil {
		return Node{}, err
	}
	class := shortClass(x.Class)
	return Node{
		Class:         class,
		Text:          x.Text,
		Desc:          x.Desc,
		ID:            shortID(x.ResourceID),
		Bounds:        b,
		Clickable:     x.Clickable,
		LongClickable: x.LongClickable,
		Focusable:     x.Focusable,
		Focused:       x.Focused,
		Scrollable:    x.Scrollable,
		Editable:      strings.Contains(class, "EditText"),
	}, nil
}

func (n Node) interesting() bool {
	if n.Bounds.Empty() {
		return false
	}
	return n.Clickable || n.LongClickable || n.Scrollable || n.Editable || n.Focusable ||
		n.Text != "" || n.Desc != ""
}

// parseBounds parses "[l,t][r,b]".
func parseBounds(s string) (Bounds, error) {
	var b Bounds
	if s == "" {
		return b, nil
	}
	if _, err := fmt.Sscanf(s, "[%d,%d][%d,%d]", &b.L, &b.T, &b.R, &b.B); err != nil {
		return b, fmt.Errorf("bad bounds %q: %w", s, err)
	}
	return b, nil
}

func shortClass(c string) string {
	return c[strings.LastIndexByte(c, '.')+1:]
}

func shortID(id string) string {
	if _, after, ok := strings.Cut(id, ":id/"); ok {
		return after
	}
	return id
}
