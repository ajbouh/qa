package watch

import (
	"qa/tapjio"
	"sort"
)

func sortedSetInsert(s []tapjio.TestFilter, f tapjio.TestFilter) ([]tapjio.TestFilter, bool) {
	l := len(s)
	if l == 0 {
		return []tapjio.TestFilter{f}, true
	}

	i := sort.Search(l, func(i int) bool { return s[i].GreaterThanOrEqual(f) })
	if i == l { // not found = new value is the smallest
		return append(s, f), true
	}

	if s[i] == f {
		return s, false
	}

	if i == l-1 { // new value is the biggest
		s = append(s, s[l-1])
		copy(s[1:], s[:l-1])
		s[0] = f
		return s, true
	}

	return append(append(s[0:i], f), s[i:]...), true
}

type filterSet struct {
	m []tapjio.TestFilter
}

func newFilterSet() *filterSet {
	return &filterSet{
		m: []tapjio.TestFilter{},
	}
}

func (set *filterSet) TrimCapacity() {
	if cap(set.m) == len(set.m) {
		return
	}

	m := make([]tapjio.TestFilter, len(set.m))
	copy(m, set.m)
	set.m = m
}

func (set *filterSet) Add(p tapjio.TestFilter) bool {
	m, added := sortedSetInsert(set.m, p)
	if added {
		set.m = m
	}

	return added
}

func (set *filterSet) Contains(p tapjio.TestFilter) bool {
	l := len(set.m)
	if l == 0 {
		return false
	}

	i := sort.Search(l, func(i int) bool { return set.m[i].GreaterThanOrEqual(p) })
	if i == l {
		return false
	}

	return set.m[i] == p
}

func (set *filterSet) Remove(p tapjio.TestFilter) bool {
	l := len(set.m)
	if l == 0 {
		return false
	}

	i := sort.Search(l, func(i int) bool { return set.m[i].GreaterThanOrEqual(p) })
	if i == l {
		return false
	}

	if set.m[i] != p {
		return false
	}

	set.m = append(set.m[0:i], set.m[i+1:]...)
	return true
}

func (set *filterSet) Len() int {
	return len(set.m)
}

func (set *filterSet) Slice() []tapjio.TestFilter {
	slice := make([]tapjio.TestFilter, len(set.m))
	for i, s := range set.m {
		slice[i] = s
	}

	return slice
}

func (set *filterSet) StringSlice() []string {
	slice := make([]string, len(set.m))
	for i, s := range set.m {
		slice[i] = s.String()
	}

	return slice
}
