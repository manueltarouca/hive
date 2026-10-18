package session

import (
	"errors"
	"testing"
)

func leaf(b string) *Node { return &Node{Type: "block", BlockID: b} }

func newWin(blocks ...string) *Window {
	w := &Window{ID: "window:w", Root: leaf(blocks[0]), Focus: blocks[0]}
	return w
}

func TestSplitAndRects(t *testing.T) {
	w := newWin("block:a")
	if _, err := w.Split("block:a", "right", 0.3, leaf("block:b"), "split:1"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Split("block:b", "below", 0.5, leaf("block:c"), "split:2"); err != nil {
		t.Fatal(err)
	}
	r := w.Rects()
	a, b, c := r["block:a"], r["block:b"], r["block:c"]
	if a.X != 0 || !near(a.W, 0.7) || !near(b.X, 0.7) || !near(b.W, 0.3) || !near(b.H, 0.5) || !near(c.Y, 0.5) {
		t.Fatalf("rects = %+v", r)
	}
	if got := w.Blocks(); len(got) != 3 || got[0] != "block:a" || got[2] != "block:c" {
		t.Fatalf("order = %v", got)
	}
}

func TestSplitLeftPutsNewBlockFirst(t *testing.T) {
	w := newWin("block:a")
	w.Split("block:a", "left", 0.25, leaf("block:b"), "split:1")
	r := w.Rects()
	if r["block:b"].X != 0 || !near(r["block:b"].W, 0.25) || !near(r["block:a"].X, 0.25) {
		t.Fatalf("rects = %+v", r)
	}
}

func TestRemoveCollapses(t *testing.T) {
	w := newWin("block:a")
	w.Split("block:a", "right", 0.5, leaf("block:b"), "split:1")
	w.Split("block:b", "below", 0.5, leaf("block:c"), "split:2")
	w.Focus = "block:b"
	empty := w.Remove("block:b")
	if empty {
		t.Fatal("window should not be empty")
	}
	r := w.Rects()
	if len(r) != 2 || !near(r["block:c"].H, 1) || !near(r["block:c"].X, 0.5) {
		t.Fatalf("rects = %+v", r)
	}
	if w.Focus != "block:c" {
		t.Fatalf("focus moved to %q", w.Focus)
	}
	w.Remove("block:a")
	if !w.Remove("block:c") {
		t.Fatal("window should be empty")
	}
}

func TestFocusDirection(t *testing.T) {
	w := newWin("block:a")
	w.Split("block:a", "right", 0.5, leaf("block:b"), "split:1")
	w.Split("block:b", "below", 0.5, leaf("block:c"), "split:2")
	cases := []struct{ from, dir, want string }{
		{"block:a", "right", "block:b"},
		{"block:c", "up", "block:b"},
		{"block:c", "left", "block:a"},
		{"block:a", "left", ""},
	}
	for _, c := range cases {
		if got := w.Neighbor(c.from, c.dir); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.from, c.dir, got, c.want)
		}
	}
}

func TestResizePane(t *testing.T) {
	w := newWin("block:a")
	w.Split("block:a", "right", 0.5, leaf("block:b"), "split:1")
	if err := w.ResizePane("block:a", "right", 0.1); err != nil {
		t.Fatal(err)
	}
	if !near(w.Rects()["block:a"].W, 0.6) {
		t.Fatalf("rects = %+v", w.Rects())
	}
	if err := w.ResizePane("block:a", "up", 0.1); err == nil {
		t.Fatal("no vertical border to move; want error")
	}
}

func TestResolve(t *testing.T) {
	items := []Item{
		{ID: "session:01a10cf3-2655-7e48-9b9c-43dd6612bef5", Label: "Work"},
		{ID: "session:01a10d97-414b-79ee-9988-4d96d804ef90", Label: "api"},
		{ID: "session:01a10d97-414b-79ee-9988-4d96d80abcd0", Label: "api2"},
	}
	ok := map[string]string{
		"session:01a10d97-414b-79ee-9988-4d96d804ef90": items[1].ID,
		"work": items[0].ID, "2": items[1].ID, "12bef5": items[0].ID,
	}
	for target, want := range ok {
		got, err := Resolve("session", items, target)
		if err != nil || got != want {
			t.Errorf("%q: got %q %v", target, got, err)
		}
	}
	_, err := Resolve("session", items, "nope")
	if _, ok := errors.AsType[*NotFoundError](err); !ok {
		t.Errorf("want not found, got %v", err)
	}
	_, err = Resolve("session", items, "4d96d8") // suffix of none... prefix-ish
	if err == nil {
		t.Error("want error for non-suffix")
	}
	dup := append(items, Item{ID: "session:x-1111", Label: "work"})
	_, err = Resolve("session", dup, "work")
	var amb *AmbiguousError
	if !errors.As(err, &amb) || len(amb.Matches) != 2 {
		t.Errorf("want ambiguous, got %v", err)
	}
}

func near(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }

func TestMoveBlock(t *testing.T) {
	w := newWin("block:a")
	w.Split("block:a", "right", 0.5, leaf("block:b"), "split:1")
	w.Split("block:a", "below", 0.5, leaf("block:c"), "split:2")
	// Move c from under a to below b.
	if err := w.Move("block:c", "block:b", "below", 0.5, "split:3"); err != nil {
		t.Fatal(err)
	}
	r := w.Rects()
	if !near(r["block:a"].H, 1) || !near(r["block:c"].X, 0.5) || !near(r["block:c"].Y, 0.5) {
		t.Fatalf("rects = %+v", r)
	}
	if err := w.Move("block:a", "block:a", "right", 0.5, "split:4"); err == nil {
		t.Fatal("moving a block beside itself must fail")
	}
}

// B5: a move with a bad side must not take the block out of the layout.
func TestMoveWithBadSideKeepsBlock(t *testing.T) {
	w := newWin("block:a")
	w.Split("block:a", "right", 0.5, leaf("block:b"), "split:1")
	if err := w.Move("block:b", "block:a", "diagonal", 0.5, "split:2"); err == nil {
		t.Fatal("want an error")
	}
	if !w.Has("block:b") {
		t.Fatal("block:b was orphaned")
	}
}
