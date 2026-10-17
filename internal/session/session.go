// Package session holds hive's structure: sessions contain windows (tabs),
// and each window has a layout, a binary tree of splits whose leaves are
// blocks. Everything here is plain data with no locking; the server owns the
// lock.
package session

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Session is a group of related work.
type Session struct {
	ID       string    `json:"session_id"`
	Label    string    `json:"label,omitempty"`
	Windows  []*Window `json:"windows"`
	Active   string    `json:"active_window_id,omitempty"`
	Revision uint64    `json:"revision"`
	Created  time.Time `json:"created_at"`
}

// Window is a tab: a layout of blocks.
type Window struct {
	ID     string `json:"window_id"`
	Label  string `json:"label,omitempty"`
	Root   *Node  `json:"layout"`
	Focus  string `json:"focused_block_id,omitempty"`
	Zoomed bool   `json:"zoomed,omitempty"`
}

// Node is a split or a block. Direction "horizontal" places A left of B,
// "vertical" places A above B; Ratio is A's share.
type Node struct {
	Type      string  `json:"type"` // split or block
	SplitID   string  `json:"split_id,omitempty"`
	Direction string  `json:"direction,omitempty"`
	Ratio     float64 `json:"ratio,omitempty"`
	A         *Node   `json:"a,omitempty"`
	B         *Node   `json:"b,omitempty"`
	BlockID   string  `json:"block_id,omitempty"`
}

// Rect is a block's place in its window, in fractions of the window.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Window returns the window with the ID, or nil.
func (s *Session) Window(id string) *Window {
	for _, w := range s.Windows {
		if w.ID == id {
			return w
		}
	}
	return nil
}

// WindowOf returns the window that holds a block, or nil.
func (s *Session) WindowOf(blockID string) *Window {
	for _, w := range s.Windows {
		if w.Has(blockID) {
			return w
		}
	}
	return nil
}

// ActiveWindow returns the active window, or nil.
func (s *Session) ActiveWindow() *Window {
	if w := s.Window(s.Active); w != nil {
		return w
	}
	if len(s.Windows) > 0 {
		return s.Windows[0]
	}
	return nil
}

// RemoveWindow drops a window and fixes the active one.
func (s *Session) RemoveWindow(id string) {
	for i, w := range s.Windows {
		if w.ID != id {
			continue
		}
		s.Windows = append(s.Windows[:i], s.Windows[i+1:]...)
		if s.Active == id {
			s.Active = ""
			if len(s.Windows) > 0 {
				s.Active = s.Windows[min(i, len(s.Windows)-1)].ID
			}
		}
		return
	}
}

// Blocks lists every block in the session, window by window.
func (s *Session) Blocks() []string {
	var out []string
	for _, w := range s.Windows {
		out = append(out, w.Blocks()...)
	}
	return out
}

// Blocks lists the window's blocks in layout order.
func (w *Window) Blocks() []string {
	var out []string
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.Type == "block" {
			out = append(out, n.BlockID)
			return
		}
		walk(n.A)
		walk(n.B)
	}
	walk(w.Root)
	return out
}

// Has reports whether the window holds the block.
func (w *Window) Has(blockID string) bool {
	return w.find(blockID) != nil
}

func (w *Window) find(blockID string) **Node {
	var walk func(p **Node) **Node
	walk = func(p **Node) **Node {
		n := *p
		if n == nil {
			return nil
		}
		if n.Type == "block" {
			if n.BlockID == blockID {
				return p
			}
			return nil
		}
		if r := walk(&n.A); r != nil {
			return r
		}
		return walk(&n.B)
	}
	return walk(&w.Root)
}

// Split puts sub beside the anchor block. side is right, left, below, or
// above. ratio is the new part's share of the space (0 means half).
func (w *Window) Split(anchor, side string, ratio float64, sub *Node, splitID string) (*Node, error) {
	p := w.find(anchor)
	if p == nil {
		return nil, fmt.Errorf("block %s is not in window %s", anchor, w.ID)
	}
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	dir, after, err := SideDirection(side)
	if err != nil {
		return nil, err
	}
	n := &Node{Type: "split", SplitID: splitID, Direction: dir}
	if after {
		n.A, n.B, n.Ratio = *p, sub, 1-ratio
	} else {
		n.A, n.B, n.Ratio = sub, *p, ratio
	}
	*p = n
	w.Zoomed = false
	return n, nil
}

// Move takes a block out of its place and puts it beside target.
func (w *Window) Move(blockID, target, side string, ratio float64, splitID string) error {
	if blockID == target {
		return fmt.Errorf("can't move a block beside itself")
	}
	if !w.Has(blockID) || !w.Has(target) {
		return fmt.Errorf("both blocks must be in window %s", w.ID)
	}
	if _, _, err := SideDirection(side); err != nil {
		return err // before anything is taken out of the layout
	}
	focus := w.Focus
	w.Remove(blockID)
	if _, err := w.Split(target, side, ratio, &Node{Type: "block", BlockID: blockID}, splitID); err != nil {
		return err
	}
	w.Focus = focus
	return nil
}

// SideDirection maps right/left/below/above to a split direction and whether
// the new part goes after the anchor.
func SideDirection(side string) (dir string, after bool, err error) {
	switch side {
	case "", "right":
		return "horizontal", true, nil
	case "left":
		return "horizontal", false, nil
	case "below", "down":
		return "vertical", true, nil
	case "above", "up":
		return "vertical", false, nil
	}
	return "", false, fmt.Errorf("unknown side %q: want right, left, below, or above", side)
}

// Remove takes a block out of the layout, giving its space to its sibling.
// It reports whether the window is now empty.
func (w *Window) Remove(blockID string) (empty bool) {
	if w.Root == nil {
		return true
	}
	if w.Root.Type == "block" {
		if w.Root.BlockID == blockID {
			w.Root, w.Focus = nil, ""
			return true
		}
		return false
	}
	var walk func(p **Node) bool
	walk = func(p **Node) bool {
		n := *p
		if n == nil || n.Type == "block" {
			return false
		}
		for _, side := range []struct{ child, other **Node }{{&n.A, &n.B}, {&n.B, &n.A}} {
			c := *side.child
			if c.Type == "block" && c.BlockID == blockID {
				*p = *side.other
				if w.Focus == blockID {
					w.Focus = firstBlock(*side.other)
				}
				return true
			}
		}
		return walk(&n.A) || walk(&n.B)
	}
	walk(&w.Root)
	if w.Focus == blockID {
		w.Focus = firstBlock(w.Root)
	}
	return w.Root == nil
}

func firstBlock(n *Node) string {
	for n != nil && n.Type == "split" {
		n = n.A
	}
	if n == nil {
		return ""
	}
	return n.BlockID
}

// Rects places every block in the window.
func (w *Window) Rects() map[string]Rect {
	out := map[string]Rect{}
	var walk func(n *Node, r Rect)
	walk = func(n *Node, r Rect) {
		if n == nil {
			return
		}
		if n.Type == "block" {
			out[n.BlockID] = r
			return
		}
		a, b := r, r
		if n.Direction == "horizontal" {
			a.W = r.W * n.Ratio
			b.X, b.W = r.X+a.W, r.W-a.W
		} else {
			a.H = r.H * n.Ratio
			b.Y, b.H = r.Y+a.H, r.H-a.H
		}
		walk(n.A, a)
		walk(n.B, b)
	}
	walk(w.Root, Rect{0, 0, 1, 1})
	return out
}

// Neighbor finds the nearest block in a direction: left, right, up, down.
func (w *Window) Neighbor(from, dir string) string {
	rects := w.Rects()
	r, ok := rects[from]
	if !ok {
		return ""
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	const eps = 1e-9
	best, bestDist := "", math.Inf(1)
	// Candidates in layout order, so ties go to the first one, every time.
	for _, id := range w.Blocks() {
		o := rects[id]
		if id == from {
			continue
		}
		var gap, overlap float64
		switch dir {
		case "left":
			gap, overlap = r.X-(o.X+o.W), overlapLen(r.Y, r.H, o.Y, o.H)
		case "right":
			gap, overlap = o.X-(r.X+r.W), overlapLen(r.Y, r.H, o.Y, o.H)
		case "up":
			gap, overlap = r.Y-(o.Y+o.H), overlapLen(r.X, r.W, o.X, o.W)
		case "down":
			gap, overlap = o.Y-(r.Y+r.H), overlapLen(r.X, r.W, o.X, o.W)
		default:
			return ""
		}
		if gap < -eps || overlap <= eps {
			continue
		}
		ox, oy := o.X+o.W/2, o.Y+o.H/2
		d := gap*10 + math.Hypot(ox-cx, oy-cy)
		if d < bestDist {
			best, bestDist = id, d
		}
	}
	return best
}

func overlapLen(a, al, b, bl float64) float64 {
	return math.Min(a+al, b+bl) - math.Max(a, b)
}

// ResizePane moves the nearest border of a block in a direction by delta, a
// fraction of the window.
func (w *Window) ResizePane(blockID, dir string, delta float64) error {
	want := "horizontal"
	if dir == "up" || dir == "down" {
		want = "vertical"
	} else if dir != "left" && dir != "right" {
		return fmt.Errorf("unknown direction %q", dir)
	}
	// Walk from the root to the block, remembering the deepest split of the
	// right direction on the way: that is the nearest border.
	var target *Node

	var walk func(n *Node) bool
	walk = func(n *Node) bool {
		if n == nil {
			return false
		}
		if n.Type == "block" {
			return n.BlockID == blockID
		}
		if walk(n.A) {
			if n.Direction == want && target == nil {
				target = n
			}
			return true
		}
		if walk(n.B) {
			if n.Direction == want && target == nil {
				target = n
			}
			return true
		}
		return false
	}
	if !walk(w.Root) {
		return fmt.Errorf("block %s is not in window %s", blockID, w.ID)
	}
	if target == nil {
		return fmt.Errorf("block %s has no %s border to move", blockID, dir)
	}
	// Moving right/down grows A; left/up shrinks it.
	if dir == "right" || dir == "down" {
		target.Ratio += delta
	} else {
		target.Ratio -= delta
	}
	target.Ratio = math.Max(0.05, math.Min(0.95, target.Ratio))
	return nil
}

// Item is something a target can name.
type Item struct {
	ID    string
	Label string
}

// NotFoundError means no item matched a target.
type NotFoundError struct{ Kind, Target string }

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s target %q not found", e.Kind, e.Target)
}

// AmbiguousError means several items matched a target.
type AmbiguousError struct {
	Kind, Target string
	Matches      []Item
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s target %q is ambiguous", e.Kind, e.Target)
	for _, m := range e.Matches {
		fmt.Fprintf(&b, "\n  %-12s …%s", m.Label, short(m.ID))
	}
	return b.String()
}

func short(s string) string {
	if len(s) <= 6 {
		return s
	}
	return s[len(s)-6:]
}

// ErrNoTarget is returned for an empty target.
var ErrNoTarget = errors.New("empty target")

// Resolve finds an item by full ID, then label, then 1-based position, then
// an ID suffix of at least four characters, stopping at the first kind of
// match.
func Resolve(kind string, items []Item, target string) (string, error) {
	if target == "" {
		return "", ErrNoTarget
	}
	for _, it := range items {
		if it.ID == target {
			return it.ID, nil
		}
	}
	var matches []Item
	for _, it := range items {
		if it.Label != "" && strings.EqualFold(it.Label, target) {
			matches = append(matches, it)
		}
	}
	if len(matches) == 0 {
		var n int
		if _, err := fmt.Sscanf(target, "%d", &n); err == nil && fmt.Sprint(n) == target {
			if n >= 1 && n <= len(items) {
				return items[n-1].ID, nil
			}
		}
	}
	if len(matches) == 0 && len(target) >= 4 {
		for _, it := range items {
			if strings.HasSuffix(it.ID, target) {
				matches = append(matches, it)
			}
		}
	}
	switch len(matches) {
	case 0:
		return "", &NotFoundError{kind, target}
	case 1:
		return matches[0].ID, nil
	}
	return "", &AmbiguousError{kind, target, matches}
}
