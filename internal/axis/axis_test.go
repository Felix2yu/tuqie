package axis

import "testing"

func TestStringAndParse(t *testing.T) {
	if Y.String() != "y" || X.String() != "x" {
		t.Fatalf("String: Y=%q X=%q", Y, X)
	}
	// Anything unrecognised — including upper case — is the common vertical stitch.
	cases := map[string]Axis{
		"x":          X,
		"y":          Y,
		"":           Y,
		"Y":          Y,
		"X":          Y,
		"horizontal": Y,
	}
	for s, want := range cases {
		if got := Parse(s); got != want {
			t.Fatalf("Parse(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestLengthAndAcrossAreOppositeDimensions(t *testing.T) {
	const w, h = 120, 340
	if Y.Length(w, h) != h || Y.Across(w, h) != w {
		t.Fatalf("Y: length=%d across=%d, want %d/%d", Y.Length(w, h), Y.Across(w, h), h, w)
	}
	if X.Length(w, h) != w || X.Across(w, h) != h {
		t.Fatalf("X: length=%d across=%d, want %d/%d", X.Length(w, h), X.Across(w, h), w, h)
	}
}
