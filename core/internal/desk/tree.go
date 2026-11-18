package desk

import (
	"fmt"
	"math"

	"github.com/hollis-labs/surface/module"
)

// The arrangement of the windows is a tree the server owns. A row holds rows and stacks
// side by side; a stack holds windows as tabs, one of them showing. The root row runs
// left to right and each nested row turns the other way, so a row inside the root is a
// column. Windows taken out of the tiling float over it, each float holding a small tree
// of its own. The page draws the tree and posts it back when the owner drags something;
// everything else (opening, closing, the agent arranging) changes it here.

// Node is a row of nodes or a stack of windows.
type Node struct {
	// Type is "row" or "stack".
	Type string `json:"type"`
	// ID names a stack, so the page can keep it across changes. Rows have none.
	ID string `json:"id,omitempty"`
	// Weight is the node's share of its row, relative to its siblings.
	Weight   float64 `json:"weight,omitempty"`
	Children []*Node `json:"children,omitempty"`
	// Wins are a stack's windows, in tab order; Selected is the one showing.
	Wins     []string `json:"wins,omitempty"`
	Selected int      `json:"selected,omitempty"`
	// Maximized makes a stack cover the rest of the tiling.
	Maximized bool `json:"maximized,omitempty"`
}

// Float is a window (or a few, docked together) floating over the tiling.
type Float struct {
	ID   string      `json:"id"`
	Rect module.Rect `json:"rect"`
	Root *Node       `json:"root"`
}

// Tree is the whole arrangement. Floats are in stacking order: the last is in front.
type Tree struct {
	Root   *Node    `json:"root"`
	Floats []*Float `json:"floats,omitempty"`
}

const (
	minFloatW = 320
	minFloatH = 180
)

// share turns a fraction of the width into a weight, in hundredths of a percent.
func share(f float64) float64 { return math.Round(f*10000) / 100 }

func newTree() Tree { return Tree{Root: &Node{Type: "row"}} }

func (t *Tree) clone() Tree {
	out := Tree{Root: cloneNode(t.Root)}
	for _, f := range t.Floats {
		out.Floats = append(out.Floats, &Float{ID: f.ID, Rect: f.Rect, Root: cloneNode(f.Root)})
	}
	return out
}

func cloneNode(n *Node) *Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Wins = append([]string(nil), n.Wins...)
	c.Children = nil
	for _, ch := range n.Children {
		c.Children = append(c.Children, cloneNode(ch))
	}
	return &c
}

// walk visits every stack under n, in document order, until fn returns false.
func walk(n *Node, fn func(stack *Node) bool) bool {
	if n == nil {
		return true
	}
	if n.Type == "stack" {
		return fn(n)
	}
	for _, c := range n.Children {
		if !walk(c, fn) {
			return false
		}
	}
	return true
}

// locate finds the stack holding a window, and the float it is in when it floats.
func (t *Tree) locate(id string) (stack *Node, float *Float, at int) {
	find := func(root *Node) bool {
		return !walk(root, func(s *Node) bool {
			for i, w := range s.Wins {
				if w == id {
					stack, at = s, i
					return false
				}
			}
			return true
		})
	}
	if find(t.Root) {
		return stack, nil, at
	}
	for _, f := range t.Floats {
		if find(f.Root) {
			return stack, f, at
		}
	}
	return nil, nil, -1
}

// order lists the windows in document order: the tiling first, then the floats.
func (t *Tree) order() []string {
	var out []string
	add := func(s *Node) bool { out = append(out, s.Wins...); return true }
	walk(t.Root, add)
	for _, f := range t.Floats {
		walk(f.Root, add)
	}
	return out
}

// tiled lists the stacks of the tiling, in document order.
func (t *Tree) tiled() []*Node {
	var out []*Node
	walk(t.Root, func(s *Node) bool { out = append(out, s); return true })
	return out
}

// floating reports whether a window floats.
func (t *Tree) floating(id string) bool {
	_, f, _ := t.locate(id)
	return f != nil
}

// prune drops empty stacks and rows, and keeps every stack's selection in range.
func prune(n *Node) *Node {
	if n == nil {
		return nil
	}
	if n.Type == "stack" {
		if len(n.Wins) == 0 {
			return nil
		}
		if n.Selected < 0 || n.Selected >= len(n.Wins) {
			n.Selected = len(n.Wins) - 1
		}
		return n
	}
	kept := n.Children[:0]
	for _, c := range n.Children {
		if p := prune(c); p != nil {
			kept = append(kept, p)
		}
	}
	n.Children = kept
	if len(kept) == 0 {
		return nil
	}
	return n
}

// fold replaces a row inside the tree that holds a single stack with that stack: a column
// of one is just the window. The root stays a row whatever it holds.
func fold(n *Node) {
	for i, c := range n.Children {
		if c.Type != "row" {
			continue
		}
		fold(c)
		if len(c.Children) == 1 && c.Children[0].Type == "stack" {
			only := c.Children[0]
			only.Weight = c.Weight
			n.Children[i] = only
		}
	}
}

func (t *Tree) tidy() {
	if t.Root = prune(t.Root); t.Root == nil {
		t.Root = &Node{Type: "row"}
	}
	fold(t.Root)
	floats := t.Floats[:0]
	for _, f := range t.Floats {
		if f.Root = prune(f.Root); f.Root != nil {
			floats = append(floats, f)
		}
	}
	t.Floats = floats
}

// remove takes a window out of the arrangement.
func (t *Tree) remove(id string) {
	s, _, at := t.locate(id)
	if s == nil {
		return
	}
	s.Wins = append(s.Wins[:at], s.Wins[at+1:]...)
	if s.Selected > at || s.Selected >= len(s.Wins) {
		s.Selected = max(0, s.Selected-1)
	}
	if len(s.Wins) == 0 {
		s.Maximized = false
	}
	t.tidy()
}

// add tiles a window the way the desk always has: the first fills the desk, the second
// takes the right-hand side, and the rest stack below it. ratio is the first window's
// share of the width when the second arrives.
func (t *Tree) add(id, stackID string, ratio float64) {
	s := &Node{Type: "stack", ID: stackID, Wins: []string{id}}
	stacks := t.tiled()
	switch len(stacks) {
	case 0:
		t.Root = &Node{Type: "row", Children: []*Node{s}}
		return
	case 1:
		if len(t.Root.Children) == 1 && t.Root.Children[0] == stacks[0] {
			stacks[0].Weight = share(ratio)
			s.Weight = share(1 - ratio)
			t.Root.Children = append(t.Root.Children, s)
			return
		}
	}
	last := stacks[len(stacks)-1]
	parent, depth := parentOf(t.Root, last, 0)
	if parent == nil {
		t.Root.Children = append(t.Root.Children, s)
		return
	}
	if depth%2 == 1 {
		// The parent is a column: the new window goes at the bottom of it.
		parent.Children = append(parent.Children, s)
		return
	}
	// The parent runs left to right: the last stack becomes a column of two.
	for i, c := range parent.Children {
		if c == last {
			col := &Node{Type: "row", Weight: last.Weight, Children: []*Node{last, s}}
			last.Weight = 0
			parent.Children[i] = col
			return
		}
	}
}

// parentOf finds the row holding a node and that row's depth (the root is 0).
func parentOf(n, target *Node, depth int) (*Node, int) {
	for _, c := range n.Children {
		if c == target {
			return n, depth
		}
		if c.Type == "row" {
			if p, d := parentOf(c, target, depth+1); p != nil {
				return p, d
			}
		}
	}
	return nil, 0
}

// float takes a window out of the tiling to float at rect.
func (t *Tree) float(id, floatID, stackID string, rect module.Rect) {
	t.remove(id)
	rect.W, rect.H = max(rect.W, minFloatW), max(rect.H, minFloatH)
	t.Floats = append(t.Floats, &Float{ID: floatID, Rect: rect,
		Root: &Node{Type: "row", Children: []*Node{{Type: "stack", ID: stackID, Wins: []string{id}}}}})
}

// show makes a window the one showing in its stack, brings its float to the front, and
// lifts a maximized stack that would hide it.
func (t *Tree) show(id string) {
	s, f, at := t.locate(id)
	if s == nil {
		return
	}
	s.Selected = at
	if f != nil {
		for i, x := range t.Floats {
			if x == f {
				t.Floats = append(append(t.Floats[:i:i], t.Floats[i+1:]...), f)
				break
			}
		}
		return
	}
	for _, other := range t.tiled() {
		if other != s {
			other.Maximized = false
		}
	}
}

// swap exchanges the places of two windows.
func (t *Tree) swap(a, b string) {
	sa, _, ia := t.locate(a)
	sb, _, ib := t.locate(b)
	if sa == nil || sb == nil {
		return
	}
	sa.Wins[ia], sb.Wins[ib] = b, a
}

// setRatio gives the first node of the tiling this share of the width.
func (t *Tree) setRatio(ratio float64) {
	kids := t.Root.Children
	if len(kids) < 2 {
		return
	}
	kids[0].Weight = share(ratio)
	rest := share((1 - ratio) / float64(len(kids)-1))
	for _, k := range kids[1:] {
		k.Weight = rest
	}
}

// normalize makes a tree that came from outside (the page, a saved layout) safe to keep:
// it holds each of the known windows exactly once and nothing else. Windows it does not
// place are tiled in; names is called for each new stack or float id.
func (t *Tree) normalize(known []string, ratio float64, names func(kind string) string) {
	want := map[string]bool{}
	for _, id := range known {
		want[id] = true
	}
	seen := map[string]bool{}
	stackIDs := map[string]bool{}
	maximized := false
	var fix func(n *Node, depth int)
	fix = func(n *Node, depth int) {
		if n.Type != "stack" {
			n.Type = "row"
			n.ID, n.Wins, n.Selected, n.Maximized = "", nil, 0, false
			for _, c := range n.Children {
				if c != nil && depth < 12 {
					fix(c, depth+1)
				}
			}
			return
		}
		n.Children = nil
		selected := ""
		if n.Selected >= 0 && n.Selected < len(n.Wins) {
			selected = n.Wins[n.Selected]
		}
		kept := n.Wins[:0]
		for _, w := range n.Wins {
			if want[w] && !seen[w] {
				seen[w] = true
				kept = append(kept, w)
			}
		}
		n.Wins = kept
		n.Selected = 0
		for i, w := range kept {
			if w == selected {
				n.Selected = i
			}
		}
		if n.ID == "" || stackIDs[n.ID] {
			n.ID = names("stack")
		}
		stackIDs[n.ID] = true
		if n.Weight < 0 {
			n.Weight = 0
		}
		// Only one stack is maximized, and only in the tiling.
		if n.Maximized && maximized {
			n.Maximized = false
		}
		maximized = maximized || n.Maximized
	}
	if t.Root == nil {
		t.Root = &Node{Type: "row"}
	}
	fix(t.Root, 0)
	t.Root.Weight = 0
	floatIDs := map[string]bool{}
	floats := t.Floats[:0]
	for _, f := range t.Floats {
		if f == nil || f.Root == nil {
			continue
		}
		fix(f.Root, 0)
		walk(f.Root, func(s *Node) bool { s.Maximized = false; return true })
		if f.ID == "" || floatIDs[f.ID] {
			f.ID = names("float")
		}
		floatIDs[f.ID] = true
		f.Rect.W, f.Rect.H = max(f.Rect.W, minFloatW), max(f.Rect.H, minFloatH)
		f.Rect.X, f.Rect.Y = max(f.Rect.X, 0), max(f.Rect.Y, 0)
		floats = append(floats, f)
	}
	t.Floats = floats
	t.tidy()
	for _, id := range known {
		if !seen[id] {
			t.add(id, names("stack"), ratio)
		}
	}
}

// compose builds the arrangement for a set of windows from scratch: tiled, or each
// floating at the rectangle given for it.
func compose(ids []string, floats map[string]module.Rect, ratio float64, names func(kind string) string) Tree {
	t := newTree()
	for _, id := range ids {
		if _, ok := floats[id]; !ok {
			t.add(id, names("stack"), ratio)
		}
	}
	for _, id := range ids {
		if r, ok := floats[id]; ok {
			t.float(id, names("float"), names("stack"), r)
		}
	}
	return t
}

// template turns the tree into one that names windows by their place in a list, so it can
// be saved with a layout and laid out again over new windows.
func (t *Tree) template(index map[string]int) Tree {
	out := t.clone()
	rename := func(s *Node) bool {
		for i, w := range s.Wins {
			s.Wins[i] = fmt.Sprintf("#%d", index[w])
		}
		return true
	}
	walk(out.Root, rename)
	for _, f := range out.Floats {
		walk(f.Root, rename)
	}
	return out
}

// instantiate is the reverse of template: ids[i] takes the place of "#i". A place with no
// window is dropped by the normalize that follows.
func (t *Tree) instantiate(ids []string) {
	rename := func(s *Node) bool {
		for i, w := range s.Wins {
			var n int
			if _, err := fmt.Sscanf(w, "#%d", &n); err == nil && n >= 0 && n < len(ids) {
				s.Wins[i] = ids[n]
			} else {
				s.Wins[i] = ""
			}
		}
		return true
	}
	walk(t.Root, rename)
	for _, f := range t.Floats {
		walk(f.Root, rename)
	}
}
