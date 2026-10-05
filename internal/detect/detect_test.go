package detect

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
	"time"

	"tuqie/internal/axis"
)

// stitched builds a long image from bands of photo-like content separated by
// plain gaps. Each photo has its own colour cast, the way real pictures do, so
// the detector can compare the composition on either side of a candidate line.
// Bands run top to bottom for axis.Y and left to right for axis.X.
func stitched(width int, sizes []int, gap int, seed int64, ax axis.Axis) *image.RGBA {
	total := len(sizes)*gap
	for _, s := range sizes {
		total += s
	}
	img := image.NewRGBA(image.Rect(0, 0, width, total))
	if ax == axis.X {
		img = image.NewRGBA(image.Rect(0, 0, total, width))
	}
	put := func(perp, along int, c color.RGBA) {
		if ax == axis.X {
			img.SetRGBA(along, perp, c)
			return
		}
		img.SetRGBA(perp, along, c)
	}
	rng := rand.New(rand.NewSource(seed))
	p := 0
	for _, s := range sizes {
		base := [3]int{40 + rng.Intn(170), 40 + rng.Intn(170), 40 + rng.Intn(170)}
		for dp := 0; dp < s; dp++ {
			for x := 0; x < width; x++ {
				tx := (x*3 + dp) % 64
				put(x, p, color.RGBA{
					R: channel(base[0] + tx*2),
					G: channel(base[1] + tx),
					B: channel(base[2] + 63 - tx),
					A: 255,
				})
			}
			p++
		}
		for dp := 0; dp < gap; dp++ {
			for x := 0; x < width; x++ {
				put(x, p, color.RGBA{255, 255, 255, 255})
			}
			p++
		}
	}
	return img
}

func channel(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func seamCenters(sizes []int, gap int) []int {
	var out []int
	p := 0
	for i, s := range sizes {
		p += s
		if i < len(sizes)-1 {
			out = append(out, p+gap/2)
		}
		p += gap
	}
	return out
}

func TestAnalyzeFindsSeams(t *testing.T) {
	sizes := []int{700, 520, 900, 640}
	gap := 24
	img := stitched(600, sizes, gap, 7, axis.Y)
	want := seamCenters(sizes, gap)

	res := Analyze(img, DefaultOptions())
	if res.Axis != axis.Y {
		t.Fatalf("picked axis %s for a vertical stitch", res.Axis)
	}
	if len(res.Candidates) != len(want) {
		t.Fatalf("got %d candidates %v, want %d", len(res.Candidates), positions(res.Candidates), len(want))
	}
	for i, w := range want {
		got := res.Candidates[i]
		if absInt(got.Pos-w) > gap {
			t.Errorf("candidate %d at %d, want near %d (+-%d)", i, got.Pos, w, gap)
		}
		if !got.Blank {
			t.Errorf("candidate %d at %d should snap onto the blank gap", i, got.Pos)
		}
		if got.Score < 0.5 {
			t.Errorf("candidate %d score %.2f is suspiciously low", i, got.Score)
		}
	}
}

func TestAnalyzeFindsColumnSeams(t *testing.T) {
	// The same fixture turned sideways: photos butted left to right, which is
	// what a wide table or a landscape collage scroll capture looks like.
	sizes := []int{700, 520, 900, 640}
	gap := 24
	img := stitched(600, sizes, gap, 7, axis.X)
	want := seamCenters(sizes, gap)

	res := Analyze(img, DefaultOptions())
	if res.Axis != axis.X {
		t.Fatalf("picked axis %s for a left-to-right stitch; candidates %v", res.Axis, positions(res.Candidates))
	}
	if len(res.Candidates) != len(want) {
		t.Fatalf("got %d candidates %v, want %d", len(res.Candidates), positions(res.Candidates), len(want))
	}
	for i, w := range want {
		got := res.Candidates[i]
		if absInt(got.Pos-w) > gap {
			t.Errorf("cut %d at x=%d, want near %d (+-%d)", i, got.Pos, w, gap)
		}
		if !got.Blank {
			t.Errorf("cut %d at x=%d should snap onto the blank gap", i, got.Pos)
		}
		if got.Score < 0.5 {
			t.Errorf("cut %d score %.2f is suspiciously low", i, got.Score)
		}
	}
}

func TestAnalyzeRejectsPhotoInterior(t *testing.T) {
	// One plain photo: no seams to find, and no reason to leave the vertical default.
	img := stitched(600, []int{1800}, 0, 3, axis.Y)
	res := Analyze(img, DefaultOptions())
	if len(res.Candidates) != 0 {
		t.Fatalf("expected no candidates, got %v", positions(res.Candidates))
	}
	if res.Axis != axis.Y {
		t.Errorf("picked axis %s with nothing to say about either direction", res.Axis)
	}
	if len(res.Signal.Diff) == 0 {
		t.Fatal("empty signal")
	}
}

func TestAnalyzeTallScreenshot(t *testing.T) {
	// 1080x21600, six photos: the shape of a real stitched scroll capture.
	sizes := []int{3600, 3600, 3600, 3600, 3600, 3600}
	img := stitched(1080, sizes, 12, 11, axis.Y)
	want := seamCenters(sizes, 12)

	start := time.Now()
	res := Analyze(img, DefaultOptions())
	elapsed := time.Since(start)
	t.Logf("analyze 1080x21600 took %s, found %d candidates", elapsed.Round(time.Millisecond), len(res.Candidates))
	if elapsed > 3*time.Second {
		t.Errorf("too slow: %s", elapsed)
	}
	if res.Axis != axis.Y {
		t.Fatalf("picked axis %s, candidates %v", res.Axis, positions(res.Candidates))
	}
	if len(res.Candidates) != len(want) {
		t.Fatalf("got %v, want %d seams", positions(res.Candidates), len(want))
	}
	for i, w := range want {
		if absInt(res.Candidates[i].Pos-w) > 20 {
			t.Errorf("seam %d: got %d want %d", i, res.Candidates[i].Pos, w)
		}
	}
}

func TestAnalyzeWideScreenshot(t *testing.T) {
	// The horizontal twin of the tall case: 21600x1080, six photos.
	sizes := []int{3600, 3600, 3600, 3600, 3600, 3600}
	img := stitched(1080, sizes, 12, 11, axis.X)
	want := seamCenters(sizes, 12)

	start := time.Now()
	res := Analyze(img, DefaultOptions())
	t.Logf("analyze 21600x1080 took %s, found %d candidates", time.Since(start).Round(time.Millisecond), len(res.Candidates))
	if res.Axis != axis.X {
		t.Fatalf("picked axis %s, candidates %v", res.Axis, positions(res.Candidates))
	}
	if len(res.Candidates) != len(want) {
		t.Fatalf("got %v, want %d seams", positions(res.Candidates), len(want))
	}
	for i, w := range want {
		if absInt(res.Candidates[i].Pos-w) > 20 {
			t.Errorf("seam %d: got x=%d want %d", i, res.Candidates[i].Pos, w)
		}
	}
}

func TestAnalyzeGaplessStitch(t *testing.T) {
	// Photos butted together with no padding: only the content jump is left, so
	// these lines land in the lower confidence tier.
	sizes := []int{700, 520, 900, 640}
	img := stitched(600, sizes, 0, 7, axis.Y)
	want := seamCenters(sizes, 0)

	res := Analyze(img, DefaultOptions())
	if len(res.Candidates) < len(want) {
		t.Fatalf("got %v, want at least %d seams", positions(res.Candidates), len(want))
	}
	for _, c := range res.Candidates {
		if c.Kind == "gap" {
			t.Errorf("candidate at %d claims a blank band that is not there", c.Pos)
		}
		if c.Score > 0.55 {
			t.Errorf("candidate at %d score %.2f outranks the gap tier", c.Pos, c.Score)
		}
	}

	// The default sensitivity must keep the real seams and drop the rest.
	var confident []Candidate
	for _, c := range res.Candidates {
		if c.Score >= 0.45 {
			confident = append(confident, c)
		}
	}
	if len(confident) != len(want) {
		t.Fatalf("confident candidates %v, want %d", positions(confident), len(want))
	}
	for i, c := range confident {
		if absInt(c.Pos-want[i]) > 30 {
			t.Errorf("confident candidate %d at %d, want near %d", i, c.Pos, want[i])
		}
	}
}

func positions(c []Candidate) []int {
	out := make([]int, len(c))
	for i, v := range c {
		out[i] = v.Pos
	}
	return out
}
