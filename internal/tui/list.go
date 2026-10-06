package tui

import "strings"

type loadState uint8

const (
	stateIdle loadState = iota // never requested: no parent selection yet
	stateLoading
	stateLoaded
	stateFailed
)

// list is one pane's worth of remote data plus the cursor over it. rows holds
// the indexes of the items that match query, and cursor points into rows.
type list[T any] struct {
	state  loadState
	err    error
	items  []T
	rows   []int
	cursor int
	gen    int // generation of the newest request; other generations are stale
	query  string
	label  func(T) string
}

// begin records that a request is going out and returns the generation its
// reply must carry to be accepted.
func (l *list[T]) begin() int {
	l.gen++
	l.state, l.err = stateLoading, nil
	return l.gen
}

// accept applies a reply, reporting whether it was current. A stale reply
// answers a question the user has already moved past, so it is dropped.
func (l *list[T]) accept(gen int, items []T, err error) bool {
	if gen != l.gen {
		return false
	}
	if err != nil {
		l.state, l.err, l.items, l.rows, l.cursor = stateFailed, err, nil, nil, 0
		return true
	}
	l.state, l.err, l.items = stateLoaded, nil, items
	l.refilter()
	l.cursor = clamp(l.cursor, len(l.rows))
	return true
}

// reset blanks the list and invalidates anything in flight for it. The filter
// survives, so it still applies to whatever loads next.
func (l *list[T]) reset() { l.gen++; *l = list[T]{gen: l.gen, query: l.query, label: l.label} }

// setQuery keeps the selected item selected when it still matches, and
// otherwise moves to the first match.
func (l *list[T]) setQuery(query string) {
	selected := -1
	if l.cursor >= 0 && l.cursor < len(l.rows) {
		selected = l.rows[l.cursor]
	}
	l.query = query
	l.refilter()
	l.cursor = 0
	for i, index := range l.rows {
		if index == selected {
			l.cursor = i
			break
		}
	}
}

func (l *list[T]) refilter() {
	needle := strings.ToLower(l.query)
	rows := make([]int, 0, len(l.items))
	for i, item := range l.items {
		if needle == "" || l.label == nil || strings.Contains(strings.ToLower(l.label(item)), needle) {
			rows = append(rows, i)
		}
	}
	l.rows = rows
}

func (l *list[T]) move(delta int) { l.cursor = clamp(l.cursor+delta, len(l.rows)) }

func (l list[T]) current() (T, bool) {
	if l.cursor < 0 || l.cursor >= len(l.rows) {
		var zero T
		return zero, false
	}
	return l.items[l.rows[l.cursor]], true
}

func (l list[T]) visible() []T {
	items := make([]T, len(l.rows))
	for i, index := range l.rows {
		items[i] = l.items[index]
	}
	return items
}

func clamp(value, count int) int {
	if count == 0 || value < 0 {
		return 0
	}
	if value >= count {
		return count - 1
	}
	return value
}
