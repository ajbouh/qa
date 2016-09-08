package watch

import (
	"qa/tapjio"
)

type filePathSet map[tapjio.FilePath]struct{}

func newFilePathSet() filePathSet {
	return filePathSet(make(map[tapjio.FilePath]struct{}))
}

func (set filePathSet) Add(p tapjio.FilePath) bool {
	_, present := set[p]

	if present {
		return false
	}

	set[p] = struct{}{}
	return true
}

func (set filePathSet) Contains(p tapjio.FilePath) bool {
	_, present := set[p]
	return present
}

func (set filePathSet) Remove(p tapjio.FilePath) bool {
	_, present := set[p]
	if present {
		delete(set, p)
	}
	return present
}

func (set filePathSet) Len() int {
	return len(set)
}

func (set filePathSet) Slice() []tapjio.FilePath {
	slice := make([]tapjio.FilePath, len(set))
	i := 0
	for s := range set {
		slice[i] = s
		i++
	}

	return slice
}

func (set filePathSet) StringSlice() []string {
	slice := make([]string, len(set))
	i := 0
	for s := range set {
		slice[i] = string(s)
		i++
	}

	return slice
}
