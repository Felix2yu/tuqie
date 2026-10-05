// Package axis names the direction a long screenshot was stitched, which is the
// direction its cut lines run across.
package axis

// Axis is the direction cut positions advance in.
type Axis int

const (
	// Y is a top-to-bottom stitch: cuts are rows, positions are y.
	Y Axis = iota
	// X is a left-to-right stitch: cuts are columns, positions are x.
	X
)

func (a Axis) String() string {
	if a == X {
		return "x"
	}
	return "y"
}

// Parse reads the wire form; anything unrecognised is the common vertical stitch.
func Parse(s string) Axis {
	if s == "x" {
		return X
	}
	return Y
}

// Length is how far cut positions run: the image width for X, its height for Y.
func (a Axis) Length(width, height int) int {
	if a == X {
		return width
	}
	return height
}

// Across is the dimension a scan line is sampled along — the other one.
func (a Axis) Across(width, height int) int {
	if a == X {
		return height
	}
	return width
}
