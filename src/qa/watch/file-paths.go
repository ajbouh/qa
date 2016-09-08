package watch

import (
	"qa/tapjio"
)

func filePathSlicesEq(a, b []tapjio.FilePath) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
