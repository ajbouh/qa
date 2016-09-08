package watch

import (
	"qa/tapjio"
	"testing"
)

func TestFilterSet(t *testing.T) {
	set := newFilterSet()

	f1 := tapjio.TestFilter("f1")
	f2 := tapjio.TestFilter("f2")
	f3 := tapjio.TestFilter("f3")

	if set.Contains(f1) {
		t.Fatal("Falsely claims to already contain f1")
	}
	if set.Contains(f2) {
		t.Fatal("Falsely claims to already contain f2")
	}
	if set.Len() != 0 {
		t.Fatalf("Initial length is wrong, got %d, expected 0", set.Len())
	}
	initialSlice := set.Slice()
	if len(initialSlice) != 0 {
		t.Fatalf("Initial Slice() length is wrong, got %d, expected 0. Slice() => %#v", len(initialSlice), initialSlice)
	}

	initialStringSlice := set.StringSlice()
	if len(initialStringSlice) != 0 {
		t.Fatalf("Initial StringSlice() length is wrong, got %d, expected 0. StringSlice() => %#v", len(initialStringSlice), initialStringSlice)
	}

	if !set.Add(f1) {
		t.Fatal("Expected Add(...) of new item to return true")
	}
	if !set.Contains(f1) {
		t.Fatal("Expected Contains(...) of new item to return true")
	}
	if set.Contains(f2) {
		t.Fatal("Expected Contains(...) of unadded item to return false")
	}
	if set.Len() != 1 {
		t.Fatalf("Length after 1 Add(...) is wrong, got %d, expected 1", set.Len())
	}
	if len(initialSlice) != 0 {
		t.Fatalf("Length of initially empty Slice() grew after 1 Add(...), got %d, expected 0", len(initialSlice))
	}
	if len(initialStringSlice) != 0 {
		t.Fatalf("Length of initially empty StringSlice() grew after 1 Add(...), got %d, expected 0", len(initialStringSlice))
	}
	slice := set.Slice()
	if len(slice) != set.Len() {
		t.Fatalf("Length of Slice() is wrong. Got %d, expected %d. Slice() => %#v", len(slice), set.Len(), slice)
	}

	stringSlice := set.StringSlice()
	if len(stringSlice) != set.Len() {
		t.Fatalf("Length of StringSlice() is wrong. Got %d, expected %d. StringSlice() => %#v", len(stringSlice), set.Len(), stringSlice)
	}

	if set.Add(f1) {
		t.Fatal("Duplicate Add(...) should not return true")
	}
	if set.Len() != 1 {
		t.Fatal("Length should not change after duplicate Add(...)")
	}

	slice = set.Slice()
	if len(slice) != set.Len() {
		t.Fatalf("Length of Slice() is wrong. Got %d, expected %d. Slice() => %#v", len(slice), set.Len(), slice)
	}

	stringSlice = set.StringSlice()
	if len(stringSlice) != set.Len() {
		t.Fatalf("Length of StringSlice() is wrong. Got %d, expected %d. StringSlice() => %#v", len(stringSlice), set.Len(), stringSlice)
	}

	set.Add(f2)
	if set.Len() != 2 {
		t.Fatalf("Length after second Add(...) is wrong. Got %d, expected 2.", set.Len())
	}
	if !set.Contains(f1) {
		t.Fatalf("Expected Contains(...) of initially added item to return true. %#v", set)
	}
	if !set.Contains(f2) {
		t.Fatal("Expected Contains(...) of second added item to return true")
	}

	maxLenSlice := set.Slice()
	if len(maxLenSlice) != set.Len() {
		t.Fatal()
	}
	maxLenStringSlice := set.StringSlice()
	if len(maxLenStringSlice) != set.Len() {
		t.Fatal()
	}

	if set.Remove(f3) {
		t.Fatalf("Expected removal of unadded item to return false")
	}
	if set.Len() != 2 {
		t.Fatal()
	}

	if !set.Remove(f2) {
		t.Fatalf("Expected removal of existing item to return true")
	}
	if set.Len() != 1 {
		t.Fatalf("Expected length after removal to be 1. Got: %d", set.Len())
	}
	if len(set.Slice()) != set.Len() {
		t.Fatal()
	}
	if len(set.StringSlice()) != set.Len() {
		t.Fatal()
	}

	if !set.Remove(f1) {
		t.Fatalf("Expected removal of initially added item to return true")
	}
	if set.Remove(f1) {
		t.Fatalf("Expected re-removal of initially added item to return false")
	}
	if set.Len() != 0 {
		t.Fatal()
	}
	if len(set.Slice()) != set.Len() {
		t.Fatal()
	}
	if len(set.StringSlice()) != set.Len() {
		t.Fatal()
	}

	if len(maxLenSlice) != 2 {
		t.Fatal()
	}
	if len(maxLenStringSlice) != 2 {
		t.Fatal()
	}

	set.Add(f3)
	if !set.Contains(f3) {
		t.Fatal()
	}
	if set.Len() != 1 {
		t.Fatal()
	}
}
