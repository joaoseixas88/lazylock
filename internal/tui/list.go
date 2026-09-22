package tui

type loadState uint8

const (
	stateIdle loadState = iota // never requested: no parent selection yet
	stateLoading
	stateLoaded
	stateFailed
)

// list is one pane's worth of remote data plus the cursor over it.
type list[T any] struct {
	state  loadState
	err    error
	items  []T
	cursor int
	gen    int // generation of the newest request; other generations are stale
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
		l.state, l.err, l.items, l.cursor = stateFailed, err, nil, 0
		return true
	}
	l.state, l.err, l.items = stateLoaded, nil, items
	l.cursor = clamp(l.cursor, len(items))
	return true
}

// reset blanks the list and invalidates anything in flight for it.
func (l *list[T]) reset() { l.gen++; *l = list[T]{gen: l.gen} }

func (l *list[T]) move(delta int) { l.cursor = clamp(l.cursor+delta, len(l.items)) }

func (l list[T]) current() (T, bool) {
	if l.cursor < 0 || l.cursor >= len(l.items) {
		var zero T
		return zero, false
	}
	return l.items[l.cursor], true
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
