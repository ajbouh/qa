package glob

import (
	"testing"
)

type globSubpatternTest struct {
	pattern  string
	expanded []string
	err      error
}

var globSubpatternTests = []globSubpatternTest{
	{"abc", []string{"abc"}, nil},
	{"{abc}", []string{"abc"}, nil},
	{"a{bc}", []string{"abc"}, nil},
	{"a{b,c}", []string{"ab", "ac"}, nil},
	{"a{b,c}de", []string{"abde", "acde"}, nil},
	{"a{b,c}{d,e}", []string{"abd", "abe", "acd", "ace"}, nil},
	{"a{b,c}{}", []string{"ab", "ac"}, nil},
	{"a{b,c}{}{e}f", []string{"abef", "acef"}, nil},
	{"a*{b,c}{}{*e,*g,h}f", []string{"a*b*ef", "a*b*gf", "a*bhf", "a*c*ef", "a*c*gf", "a*chf"}, nil},
	{"{a,b}{}{c,}d", []string{"acd", "ad", "bcd", "bd"}, nil},
}

func TestExpandGlobSubpatterns(t *testing.T) {
	for idx, tt := range globSubpatternTests {
		testExpandGlobSubpatternsWith(t, idx, tt)
	}
}

func stringSlicesEq(a, b []string) bool {
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

func testExpandGlobSubpatternsWith(t *testing.T, idx int, tt globSubpatternTest) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("#%v. ExpandGlobSubpatterns(%#q) panicked: %#v", idx, tt.pattern, r)
		}
	}()

	result, err := ExpandGlobSubpatterns(tt.pattern)
	if !stringSlicesEq(result, tt.expanded) || err != tt.err {
		t.Errorf("#%v. ExpandGlobSubpatterns(%#q) = %v, %v want %v, %v", idx, tt.pattern, result, err, tt.expanded, tt.err)
	}
}
