// Package keymap declares pmx's structured key bindings. Each view publishes
// a single KeyMap value from which both the bottom hint bar and the whichkey
// popup are derived — no magic strings scattered elsewhere.
//
// Shape:
//
//	KeyMap
//	├── Primary []Binding          // always-visible shortcuts (hint bar)
//	└── Leader  []Entry            // triggered by <space>; may nest via Group
//
// An Entry is a tagged union of Binding (leaf, has Action) and Group
// (branch, has Children). Groups can nest arbitrarily deep — <spc> m a d …
package keymap

// Action is a leaf handler fired when the user resolves a key path to a
// Binding. It is intentionally parameter-free; views close over their own
// state when constructing the KeyMap.
type Action func() Result

// Result is what a leaf Action returns to the whichkey router, so the host
// view (and ultimately bubbletea) can react. A Result is a plain struct —
// NOT a tea.Cmd — because this package has no bubbletea import. The caller
// adapts the result into whatever command/message shape it needs.
type Result struct {
	// Message, when non-nil, is forwarded as a tea.Msg by the caller.
	Message any
	// Quit asks the program to exit.
	Quit bool
	// Pop asks the view navigator to pop the current view.
	Pop bool
	// Push, when non-nil, is a view the navigator should push. Typed as any
	// here to keep this package decoupled from the ui package.
	Push any
	// Note is an optional diagnostic string, useful for stub bindings.
	Note string
}

// Entry is either a Binding or a Group — exactly one of Action / Children
// should be set.
type Entry struct {
	// Key is the single key (or multi-char token like "esc") that triggers
	// this entry at its parent's level. For display, prefer DisplayKey.
	Key string
	// DisplayKey is the pretty rendering of Key (e.g. "↵" for "enter"). When
	// empty the Key itself is shown.
	DisplayKey string
	// Desc is the one-line description shown in the popup / hint bar.
	Desc string
	// Action, if non-nil, makes this entry a leaf.
	Action Action
	// Children, if non-empty, makes this entry a group. Pressing Key when
	// the popup is at the parent level descends into Children.
	Children []Entry
	// Accent, if true, renders the entry with the accent color (e.g. for
	// destructive actions use the Danger variant instead).
	Accent bool
	// Danger, if true, renders the entry in the danger color.
	Danger bool
}

// IsGroup reports whether this entry opens a submenu.
func (e Entry) IsGroup() bool { return len(e.Children) > 0 }

// Label returns the key as it should be displayed.
func (e Entry) Label() string {
	if e.DisplayKey != "" {
		return e.DisplayKey
	}
	return e.Key
}

// Binding is a convenience alias when an Entry is used as a top-level primary
// shortcut. It carries the same fields but conventionally has no Children.
type Binding = Entry

// KeyMap is a view's full key-binding declaration.
type KeyMap struct {
	// Primary bindings surface in the status bar. They are also matched
	// directly by the host view's Update — this package doesn't dispatch
	// them (views already handle those keys natively in their own idiom).
	Primary []Binding

	// Leader is the tree rooted under <space>. The whichkey popup walks
	// this tree based on the user's prefix path.
	Leader []Entry
}

// FindLeader walks the Leader tree following path and returns the children
// available at that depth. If the path lands on a leaf (Binding), the second
// return is that leaf's Action; otherwise action is nil.
//
// If the path cannot be resolved, ok is false.
func (k KeyMap) FindLeader(path []string) (children []Entry, action Action, ok bool) {
	cur := k.Leader
	for i, p := range path {
		var match *Entry
		for idx := range cur {
			if cur[idx].Key == p {
				match = &cur[idx]
				break
			}
		}
		if match == nil {
			return nil, nil, false
		}
		if match.IsGroup() {
			cur = match.Children
			continue
		}
		// Leaf — only valid if we're at the end of the path.
		if i == len(path)-1 {
			return nil, match.Action, true
		}
		return nil, nil, false
	}
	return cur, nil, true
}

// Lookup finds a single Entry at the given path's parent, keyed by the final
// path element. Returns the entry and ok=true if found.
func (k KeyMap) Lookup(path []string) (Entry, bool) {
	if len(path) == 0 {
		return Entry{}, false
	}
	parent := path[:len(path)-1]
	key := path[len(path)-1]

	children, _, ok := k.FindLeader(parent)
	if !ok {
		return Entry{}, false
	}
	for _, e := range children {
		if e.Key == key {
			return e, true
		}
	}
	return Entry{}, false
}
