package glob

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar"
)

func Glob(dir, pattern string) ([]string, error) {
	// Make glob absolute, using dir
	relative := !filepath.IsAbs(pattern)
	if relative && dir != "" {
		pattern = filepath.Join(dir, pattern)
	}

	// Expand glob
	globFiles, err := doublestar.Glob(pattern)
	if err != nil {
		return nil, err
	}

	// Strip prefix from glob matches if needed.
	if relative && dir != "" {
		trimPrefix := fmt.Sprintf("%s%c", dir, os.PathSeparator)
		files := []string{}
		for _, file := range globFiles {
			files = append(files, strings.TrimPrefix(file, trimPrefix))
		}
		return files, nil
	} else {
		return globFiles, nil
	}
}

func ToMatchPathFn(pattern string) (func(string) bool, error) {
	expandedPatterns, err := ExpandGlobSubpatterns(pattern)
	if err != nil {
		return nil, err
	}

	return func(path string) bool {
		for _, expandedPattern := range expandedPatterns {
			ok, _ := doublestar.PathMatch(expandedPattern, path)
			if ok {
				return true
			}
		}

		return false
	}, nil
}

// ExpandGlobSubpatterns expands patterns like "{a,b}" to []string{"a", "b"}
func ExpandGlobSubpatterns(pattern string) ([]string, error) {
	var expandedPatterns = []string{""}

	var withinSubpattern = false
	var subpatternChoiceIx = 0
	var discoveredSubpatterns []string

	var lastIx = len(pattern) - 1
	for ix, c := range pattern {
		if withinSubpattern {
			switch c {
			case ',':
			case '}':
				withinSubpattern = false
			default:
				continue
			}

			subpattern := pattern[subpatternChoiceIx:ix]
			discoveredSubpatterns = append(discoveredSubpatterns, subpattern)

			if !withinSubpattern {
				reexpandedPatterns := make([]string, 0, len(expandedPatterns)*len(discoveredSubpatterns))
				for _, expandedPattern := range expandedPatterns {
					for _, discoveredSubpattern := range discoveredSubpatterns {
						reexpandedPatterns = append(reexpandedPatterns, expandedPattern+discoveredSubpattern)
					}
				}

				expandedPatterns = reexpandedPatterns
			}

			subpatternChoiceIx = ix + 1
		} else {
			var subpattern string
			if c == '{' {
				withinSubpattern = true
				subpattern = pattern[subpatternChoiceIx:ix]
			} else if ix == lastIx {
				subpattern = pattern[subpatternChoiceIx : ix+1]
			} else {
				continue
			}

			if len(subpattern) > 0 {
				for expandedPatternIx, expandedPattern := range expandedPatterns {
					expandedPatterns[expandedPatternIx] = expandedPattern + subpattern
				}
			}

			if withinSubpattern {
				subpatternChoiceIx = ix + 1
				discoveredSubpatterns = []string{}
			}
		}
	}

	return expandedPatterns, nil
}
