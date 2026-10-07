package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func sampleOptions(t *testing.T, axis string) options {
	t.Helper()
	return options{
		out:    filepath.Join(t.TempDir(), "sample.png"),
		width:  40,
		photos: 3,
		gap:    6,
		seed:   2026,
		axis:   axis,
	}
}

// padColor is the blank band drawn between photos; the whole point of the
// generator is that these bands land where seams() reports them.
var padColor = color.RGBA{246, 246, 247, 255}

// runs reports the [start,end) spans of consecutive padding lines, read at
// pixel 0 of each line for a vertical sample or each column for a horizontal one.
func runs(img *image.RGBA, horizontal bool) [][2]int {
	n, at := img.Rect.Dy(), func(i int) bool { return img.RGBAAt(0, i) == padColor }
	if horizontal {
		n, at = img.Rect.Dx(), func(i int) bool { return img.RGBAAt(i, 0) == padColor }
	}
	var out [][2]int
	for i := 0; i < n; i++ {
		if !at(i) {
			continue
		}
		start := i
		for i+1 < n && at(i+1) {
			i++
		}
		out = append(out, [2]int{start, i + 1})
	}
	return out
}

func decodeSample(t *testing.T, path string) *image.RGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("output is not a PNG: %v", err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("decoded %T, want *image.RGBA", img)
	}
	return rgba
}

func TestGenerateVertical(t *testing.T) {
	opt := sampleOptions(t, "y")
	var report bytes.Buffer
	if err := generate(opt, &report); err != nil {
		t.Fatal(err)
	}

	img := decodeSample(t, opt.out)
	if img.Rect.Dx() != opt.width {
		t.Fatalf("width %d, want %d", img.Rect.Dx(), opt.width)
	}
	checkSummary(t, report.String(), opt, img.Rect.Size(), "y")

	got := runs(img, false)
	// One band after every photo, the last one flush with the bottom edge.
	if len(got) != opt.photos {
		t.Fatalf("found %d padding bands %v, want %d", len(got), got, opt.photos)
	}
	for i, r := range got {
		if r[1]-r[0] != opt.gap {
			t.Errorf("band %d spans %v, want %d lines", i, r, opt.gap)
		}
	}
	if bottom := got[len(got)-1]; bottom[1] != img.Rect.Dy() {
		t.Errorf("last band ends at %d, image is %d tall", bottom[1], img.Rect.Dy())
	}

	// The cut list names the middle of every band except the trailing one.
	wanted := reportedSeams(t, report.String())
	if len(wanted) != opt.photos-1 {
		t.Fatalf("summary seams %v, want %d", wanted, opt.photos-1)
	}
	for i, pos := range wanted {
		band := got[i]
		if pos < band[0] || pos >= band[1] {
			t.Errorf("seam %d at %d outside band %v", i, pos, band)
		}
		if center := (band[0] + band[1]) / 2; pos != center {
			t.Errorf("seam %d at %d, band centre is %d", i, pos, center)
		}
	}
}

// checkSummary asserts the reported path, size and axis, so the numbers a tester
// reads from stdout match the file on disk.
func checkSummary(t *testing.T, report string, opt options, size image.Point, axis string) {
	t.Helper()
	want := opt.out + ": " + strconv.Itoa(size.X) + "x" + strconv.Itoa(size.Y) +
		", " + strconv.Itoa(opt.photos) + " photos, axis=" + axis + ","
	if !strings.HasPrefix(report, want) {
		t.Fatalf("summary %q, want prefix %q", report, want)
	}
}

func TestGenerateHorizontalTransposes(t *testing.T) {
	opt := sampleOptions(t, "x")
	var report bytes.Buffer
	if err := generate(opt, &report); err != nil {
		t.Fatal(err)
	}

	img := decodeSample(t, opt.out)
	if img.Rect.Dy() != opt.width {
		t.Fatalf("height %d, want the perpendicular size %d", img.Rect.Dy(), opt.width)
	}
	// The summary's WxH tracks the transposed image, not the requested width.
	checkSummary(t, report.String(), opt, img.Rect.Size(), "x")
	got := runs(img, true)
	if len(got) != opt.photos {
		t.Fatalf("found %d padding columns %v, want %d", len(got), got, opt.photos)
	}
	for i, r := range got {
		if r[1]-r[0] != opt.gap {
			t.Errorf("column band %d spans %v, want %d lines", i, r, opt.gap)
		}
	}
}

// reportedSeams pulls the cut list off the end of the summary line.
func reportedSeams(t *testing.T, report string) []int {
	t.Helper()
	i := strings.Index(report, "seams at [")
	if i < 0 {
		t.Fatalf("no seam list in %q", report)
	}
	list := strings.TrimSuffix(report[i+len("seams at ["):], "]\n")
	if list == "" {
		return nil
	}
	fields := strings.Fields(list)
	out := make([]int, len(fields))
	for j, f := range fields {
		v, err := strconv.Atoi(f)
		if err != nil {
			t.Fatalf("seam %q: %v", f, err)
		}
		out[j] = v
	}
	return out
}

func TestGenerateRejectsUnwritablePath(t *testing.T) {
	opt := sampleOptions(t, "y")
	opt.out = filepath.Join(t.TempDir(), "nope", "sample.png")
	if err := generate(opt, &bytes.Buffer{}); err == nil {
		t.Fatal("want error for a missing parent directory")
	}
}

func TestTranspose(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 9, A: 255})
		}
	}
	dst := transpose(src)
	if dst.Rect.Dx() != 2 || dst.Rect.Dy() != 3 {
		t.Fatalf("size %v, want 2x3", dst.Rect.Size())
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			if got := dst.RGBAAt(y, x); got != src.RGBAAt(x, y) {
				t.Errorf("dst(%d,%d) = %v, want %v", y, x, got, src.RGBAAt(x, y))
			}
		}
	}
}

func TestBlend(t *testing.T) {
	a := color.RGBA{10, 20, 30, 255}
	b := color.RGBA{110, 120, 130, 255}
	if got := blend(a, b, 0); got != a {
		t.Errorf("t=0 -> %v, want %v", got, a)
	}
	if got := blend(a, b, 1); got.R != 110 || got.G != 120 || got.B != 130 {
		t.Errorf("t=1 -> %v, want b's colour", got)
	}
	if got := blend(a, b, 0.5); got.R != 60 || got.B != 80 {
		t.Errorf("t=0.5 -> %v, want the midpoint", got)
	}
	if got := blend(a, b, 0.5); got.A != 255 {
		t.Errorf("alpha %d, want 255", got.A)
	}
}

func TestCircleClipsToPhoto(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 40))
	ink := color.RGBA{200, 10, 10, 255}
	// Centre at the top edge of the photo band: the part above it must stay clean.
	circle(img, image.Rect(0, 20, 20, 40), 10, 20, 8, ink)

	blank := color.RGBA{0, 0, 0, 0}
	if got := img.RGBAAt(10, 15); got != blank {
		t.Fatalf("painted above the clip: %v", got)
	}
	if got := img.RGBAAt(10, 25); got != ink {
		t.Fatalf("circle centre row not painted: %v", got)
	}
	if got := img.RGBAAt(0, 35); got != blank {
		t.Fatalf("corner outside the radius painted: %v", got)
	}
}

func TestDrawPhotoStaysInsideItsBand(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 420, 200))
	rng := rand.New(rand.NewSource(1))
	top, height := 20, 140
	drawPhoto(img, 420, top, height, rng)

	blank := color.RGBA{0, 0, 0, 0}
	for y := 0; y < 200; y++ {
		inside := y >= top && y < top+height
		for x := 0; x < 420; x++ {
			got := img.RGBAAt(x, y)
			if inside && got == blank {
				t.Fatalf("band row %d pixel %d left blank", y, x)
			}
			if !inside && got != blank {
				t.Fatalf("drew outside the band at (%d,%d): %v", x, y, got)
			}
			if inside && got.A != 255 {
				t.Fatalf("alpha %d at (%d,%d)", got.A, x, y)
			}
		}
	}
}

func TestDrawPhotoNarrowKeepsCirclesInside(t *testing.T) {
	// The radius formula has to survive a sample far narrower than the default
	// 1080 without painting past the photo.
	img := image.NewRGBA(image.Rect(0, 0, 24, 400))
	rng := rand.New(rand.NewSource(3))
	drawPhoto(img, 24, 100, 200, rng)

	blank := color.RGBA{0, 0, 0, 0}
	for y := 0; y < 100; y++ {
		for x := 0; x < 24; x++ {
			if img.RGBAAt(x, y) != blank {
				t.Fatalf("painted above the band at (%d,%d)", x, y)
			}
		}
	}
	for y := 300; y < 400; y++ {
		for x := 0; x < 24; x++ {
			if img.RGBAAt(x, y) != blank {
				t.Fatalf("painted below the band at (%d,%d)", x, y)
			}
		}
	}
}

func TestSeams(t *testing.T) {
	cases := []struct {
		heights []int
		gap     int
		want    []int
	}{
		{[]int{100, 200, 300}, 10, []int{105, 315}},
		{[]int{100}, 10, nil},
		{[]int{100, 200}, 1, []int{100}},
	}
	for _, c := range cases {
		if got := seams(c.heights, c.gap); !reflect.DeepEqual(got, c.want) {
			t.Errorf("seams(%v, %d) = %v, want %v", c.heights, c.gap, got, c.want)
		}
	}
}
