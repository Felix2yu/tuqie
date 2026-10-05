package server

import (
	"reflect"
	"testing"

	"tuqie/internal/axis"
)

func TestBandsFromCuts(t *testing.T) {
	got := BandsFromCuts(axis.Y, []int{400, 100, 100, 0, 900, -5, 600}, 800, 600)
	want := [][2]int{{0, 100}, {100, 400}, {400, 600}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if BandsFromCuts(axis.Y, nil, 800, 600) != nil {
		t.Fatal("no cuts should produce no bands")
	}
	if BandsFromCuts(axis.Y, []int{700}, 800, 600) != nil {
		t.Fatal("out-of-range cut should be dropped")
	}
	// Along X the same list is measured against the width, not the height.
	if BandsFromCuts(axis.X, []int{700}, 800, 600) == nil {
		t.Fatal("a cut inside the width should survive on the X axis")
	}
	got = BandsFromCuts(axis.X, []int{100}, 800, 600)
	want = [][2]int{{0, 100}, {100, 800}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("X axis got %v, want %v", got, want)
	}
}
