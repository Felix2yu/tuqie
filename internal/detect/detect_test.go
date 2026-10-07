package detect

import (
	"image"
	"image/color"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"tuqie/internal/axis"
)

// stitched builds a long image from bands of photo-like content separated by
// plain gaps. Each photo has its own colour cast, the way real pictures do, so
// the detector can compare the composition on either side of a candidate line.
// Bands run top to bottom for axis.Y and left to right for axis.X.
func stitched(width int, sizes []int, gap int, seed int64, ax axis.Axis) *image.RGBA {
	total := len(sizes) * gap
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

// ---- internals ----

// smallStitched is the same fixture as the Analyze tests at a size that makes
// direct calls into the helpers cheap.
func smallStitched() *image.RGBA {
	return stitched(40, []int{120, 90, 150}, 8, 5, axis.Y)
}

func TestAnalyzeAppliesDefaultsToZeroOptions(t *testing.T) {
	// Every field left at its zero value must fall back to DefaultOptions, not
	// turn into "nothing passes the threshold".
	res := Analyze(smallStitched(), Options{})
	want := seamCenters([]int{120, 90, 150}, 8)
	if len(res.Candidates) != len(want) {
		t.Fatalf("got %v, want %v", positions(res.Candidates), want)
	}
}

func TestAnalyzeHonoursSensitivityOverrides(t *testing.T) {
	// A line loud enough to clear AbsThresh but not its neighbourhood still
	// fails: relative evidence is what rejects a photo's own busy texture.
	strict := Options{AbsThresh: 0.001, RelThresh: 1e9, MaxCandidates: 80, SamplePerLine: 20}
	if got := Analyze(smallStitched(), strict).Candidates; len(got) != 0 {
		t.Fatalf("relative threshold should reject everything, got %v", positions(got))
	}

	// With the floor dropped every jump qualifies, so MaxCandidates bounds the
	// list and MinSlicePx decides how close two cuts may sit.
	loose := Options{AbsThresh: 0.02, RelThresh: 1, MinSlicePx: 1, MaxCandidates: 2, SamplePerLine: 20}
	got := Analyze(smallStitched(), loose).Candidates
	if len(got) != 2 {
		t.Fatalf("got %v, want the list truncated to 2", positions(got))
	}
}

func TestAnalyzeAcceptsOtherImageFormats(t *testing.T) {
	src := smallStitched()
	// A JPEG decode hands over YCbCr and a GIF hands over Paletted, so neither
	// is the *image.RGBA fast path.
	grey := image.NewGray(src.Bounds())
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			grey.SetGray(x, y, color.Gray{Y: src.RGBAAt(x, y).R})
		}
	}
	res := Analyze(grey, DefaultOptions())
	want := seamCenters([]int{120, 90, 150}, 8)
	if len(res.Candidates) != len(want) {
		t.Fatalf("non-RGBA input got %v, want %v", positions(res.Candidates), want)
	}

	// Offset origins must not shift the profiles.
	shifted := image.NewRGBA(image.Rect(4, 6, 4+src.Rect.Dx(), 6+src.Rect.Dy()))
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			shifted.SetRGBA(4+x, 6+y, src.RGBAAt(x, y))
		}
	}
	res = Analyze(shifted, DefaultOptions())
	if got := positions(res.Candidates); len(got) != len(want) {
		t.Fatalf("offset origin got %v, want %v", got, want)
	}
	for i, c := range res.Candidates {
		if absInt(c.Pos-want[i]) > 8 {
			t.Errorf("offset origin seam %d at %d, want near %d", i, c.Pos, want[i])
		}
	}
}

func TestSamplePoints(t *testing.T) {
	cases := []struct {
		n, cap int
		want   []int
	}{
		{4, 10, []int{0, 1, 2, 3}},
		{3, 0, []int{0, 1, 2}},
		{3, -1, []int{0, 1, 2}},
		{0, 10, []int{}},
		{10, 4, []int{0, 3, 6, 9}},
	}
	for _, c := range cases {
		got := samplePoints(c.n, c.cap)
		if len(got) != len(c.want) {
			t.Fatalf("samplePoints(%d,%d) = %v, want %v", c.n, c.cap, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("samplePoints(%d,%d) = %v, want %v", c.n, c.cap, got, c.want)
			}
			if i > 0 && got[i] <= got[i-1] {
				t.Fatalf("samplePoints(%d,%d) = %v, not increasing", c.n, c.cap, got)
			}
		}
	}
}

func TestSmoothIgnoresShortInputs(t *testing.T) {
	short := []float64{1, 2}
	smooth(short)
	if !reflect.DeepEqual(short, []float64{1, 2}) {
		t.Fatalf("a 2-line profile was rewritten: %v", short)
	}
	long := []float64{0, 3, 0}
	smooth(long)
	if long[1] != 1 {
		t.Fatalf("expected the 3-tap average, got %v", long)
	}
}

func TestSnapToFlatBand(t *testing.T) {
	// A band is plain when flat == 1 and calm when its diff stays near zero.
	plain := func(n int) []float64 {
		v := make([]float64, n)
		for i := range v {
			v[i] = 0.5
		}
		return v
	}

	t.Run("centre of a padded band", func(t *testing.T) {
		length := 100
		flat, diff := plain(length), make([]float64, length)
		for i := 47; i <= 51; i++ {
			flat[i] = 1
		}
		pos, blank := snapToFlatBand(flat, diff, 45, length, 10)
		if !blank || pos != 49 {
			t.Fatalf("got (%d,%v), want the band centre 49", pos, blank)
		}
	})

	t.Run("too thin to be padding", func(t *testing.T) {
		length := 100
		flat, diff := plain(length), make([]float64, length)
		flat[49], flat[50] = 1, 1
		if pos, blank := snapToFlatBand(flat, diff, 48, length, 10); blank || pos != 48 {
			t.Fatalf("got (%d,%v), want the position untouched", pos, blank)
		}
	})

	t.Run("too wide is sky, not a seam", func(t *testing.T) {
		length := 400
		flat, diff := plain(length), make([]float64, length)
		for i := 10; i < 340; i++ {
			flat[i] = 1
		}
		if pos, blank := snapToFlatBand(flat, diff, 100, length, 10); blank || pos != 100 {
			t.Fatalf("got (%d,%v), want the position untouched", pos, blank)
		}
	})

	t.Run("busy inside is a photo edge", func(t *testing.T) {
		length := 100
		flat, diff := plain(length), make([]float64, length)
		for i := 47; i <= 51; i++ {
			flat[i] = 1
		}
		diff[49] = 0.5
		if pos, blank := snapToFlatBand(flat, diff, 45, length, 10); blank || pos != 45 {
			t.Fatalf("got (%d,%v), want the position untouched", pos, blank)
		}
	})

	t.Run("nothing plain nearby", func(t *testing.T) {
		length := 100
		flat, diff := plain(length), make([]float64, length)
		if pos, blank := snapToFlatBand(flat, diff, 50, length, 4); blank || pos != 50 {
			t.Fatalf("got (%d,%v), want the position untouched", pos, blank)
		}
	})

	// A peak at the very first line has no window on one side; the scan must
	// step over the out-of-range candidates instead of reading them.
	t.Run("clips to the image", func(t *testing.T) {
		length := 100
		flat, diff := plain(length), make([]float64, length)
		for i := 1; i <= 5; i++ {
			flat[i] = 1
		}
		pos, blank := snapToFlatBand(flat, diff, 0, length, 10)
		if !blank || pos != 3 {
			t.Fatalf("got (%d,%v), want 3", pos, blank)
		}
	})
}

func TestEvidencePrefersManyConfidentLines(t *testing.T) {
	// Only the part above the 0.4 confidence floor counts, and every confident
	// line adds to it: several strong reads beat one mediocre line.
	mediocre := Result{Candidates: []Candidate{{Score: 0.5}}}
	confident := Result{Candidates: []Candidate{{Score: 0.7}, {Score: 0.7}}}
	if !(evidence(confident) > evidence(mediocre)) {
		t.Fatalf("2x0.7 (%.2f) should outweigh 1x0.5 (%.2f)", evidence(confident), evidence(mediocre))
	}
	// Below the floor a line says nothing, however many there are.
	if got := evidence(Result{Candidates: []Candidate{{Score: 0.4}, {Score: 0.2}}}); got != 0 {
		t.Fatalf("below-confidence evidence = %.2f, want 0", got)
	}
	if got := evidence(Result{}); got != 0 {
		t.Fatalf("empty result = %.2f, want 0", got)
	}
}
