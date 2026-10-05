// Package detect finds the seams between photos stitched into one long screenshot.
package detect

import (
	"image"
	"image/draw"
	"math"
	"sort"

	"tuqie/internal/axis"
)

// Options tunes sensitivity. Zero values are replaced by defaults.
type Options struct {
	// MinSlicePx rejects candidate lines closer together than this, i.e. it caps
	// how many pieces a photo may be chopped into. 0 derives it from the length.
	MinSlicePx int
	// AbsThresh is the minimum line-to-line colour distance (0..1) for a seam.
	AbsThresh float64
	// RelThresh is how much louder a line must be than its neighbourhood.
	RelThresh float64
	// MaxCandidates bounds the returned lines.
	MaxCandidates int
	// SamplePerLine caps the pixels read per scan line.
	SamplePerLine int
}

func DefaultOptions() Options {
	return Options{
		MinSlicePx:    0,
		AbsThresh:     0.045,
		RelThresh:     2.6,
		MaxCandidates: 80,
		SamplePerLine: 320,
	}
}

// Candidate is one suggested cut position along Result.Axis, in source pixels.
type Candidate struct {
	Pos   int     `json:"pos"`
	Score float64 `json:"score"`
	// Kind is "seam" for a raw discontinuity and "gap" when the line was
	// resolved onto a blank band between two photos.
	Kind string `json:"kind"`
	// Blank reports the cut landing on a plain line, which never slices content.
	Blank bool `json:"blank"`
}

// Signal is the per-line profile downsampled for rendering.
type Signal struct {
	Step  int       `json:"step"`
	Lines int       `json:"lines"`
	Diff  []float64 `json:"diff"`
	Flat  []float64 `json:"flat"`
}

type Result struct {
	Width      int
	Height     int
	Axis       axis.Axis
	Candidates []Candidate
	Signal     Signal
}

const maxL1 = 765.0 // 3 channels * 255

// Analyze reads the profile in both directions and keeps the one that tells the
// stitch story better, so a scroll capture and a left-to-right one need no
// setting from the user.
func Analyze(src image.Image, opt Options) *Result {
	if opt.AbsThresh <= 0 {
		opt = DefaultOptions()
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	rgba := toRGBA(src)

	vertical := scan(rgba, b.Min, w, h, opt, axis.Y)
	horizontal := scan(rgba, b.Min, w, h, opt, axis.X)
	// Almost every upload is stitched top to bottom, so columns only win when
	// they are clearly the better read; a tie goes to rows.
	best := vertical
	if evidence(horizontal) > evidence(vertical)*1.25 {
		best = horizontal
	}
	return &best
}

// evidence weighs how much of a stitch one direction claims to explain: several
// confident lines beat one mediocre one.
func evidence(r Result) float64 {
	var s float64
	for _, c := range r.Candidates {
		s += math.Max(0, c.Score-0.4)
	}
	return s
}

func scan(rgba *image.RGBA, origin image.Point, w, h int, opt Options, ax axis.Axis) Result {
	length := ax.Length(w, h)
	across := ax.Across(w, h)
	pr := lineProfiles(rgba, origin, samplePoints(across, opt.SamplePerLine), ax, length)

	smooth(pr.diff)
	baseline := blockBaseline(pr.diff, max(64, length/24))
	compBlock := clampInt(length/30, 40, 160)

	minSlice := opt.MinSlicePx
	if minSlice <= 0 {
		minSlice = clampInt(length/60, 40, 600)
	}

	type peak struct {
		pos         int
		strength    float64
		composition float64
	}
	var peaks []peak
	for i := 1; i < length-1; i++ {
		d := pr.diff[i]
		if d < opt.AbsThresh {
			continue
		}
		ratio := d / (baseline[i] + 0.006)
		if ratio < opt.RelThresh {
			continue
		}
		if !isLocalMax(pr.diff, i, 3) {
			continue
		}
		strength := clamp01(0.55*math.Min(d/0.30, 1) + 0.45*math.Min(ratio/8, 1))
		peaks = append(peaks, peak{pos: i, strength: strength, composition: pr.composition(i, compBlock)})
	}

	// Peaks that sit beside a blank band are snapped onto its centre so the cut
	// falls in the seam's padding instead of across the photo edge. Stitched
	// photos almost always leave such a band, so a snapped peak scores in a tier
	// above a bare content jump: the caller's threshold can keep them apart.
	used := map[int]bool{}
	var out []Candidate
	for _, p := range peaks {
		pos, blank := snapToFlatBand(pr.flat, pr.diff, p.pos, length, 28)
		// A line this close to an edge would leave a sliver as its own piece.
		if pos < minSlice || length-pos < minSlice {
			continue
		}
		if used[pos] {
			continue
		}
		used[pos] = true
		// Without padding the jump itself says little; what separates a stitch
		// from a photo's own straight edge is whether the picture changed.
		kind, score := "seam", 0.15+0.40*(0.35*p.strength+0.65*p.composition)
		if blank {
			kind, score = "gap", 0.55+0.45*p.strength
		}
		out = append(out, Candidate{Pos: pos, Score: score, Kind: kind, Blank: blank})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Pos < out[j].Pos
	})
	out = suppress(out, minSlice)
	if len(out) > opt.MaxCandidates {
		out = out[:opt.MaxCandidates]
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pos < out[j].Pos })

	return Result{
		Width:      w,
		Height:     h,
		Axis:       ax,
		Candidates: out,
		Signal:     buildSignal(pr.diff, pr.flat, length),
	}
}

func toRGBA(src image.Image) *image.RGBA {
	if r, ok := src.(*image.RGBA); ok {
		return r
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// samplePoints picks the positions read along one scan line, at most cap of them.
func samplePoints(n, cap int) []int {
	if cap <= 0 || n <= cap {
		pts := make([]int, n)
		for i := range pts {
			pts[i] = i
		}
		return pts
	}
	step := int(math.Ceil(float64(n) / float64(cap)))
	pts := make([]int, 0, n/step+1)
	for i := 0; i < n; i += step {
		pts = append(pts, i)
	}
	return pts
}

// profiles holds one value per scan line, indexed by cut position along the axis.
type profiles struct {
	diff []float64 // mean L1 distance to the line before, 0..1
	flat []float64 // 1 = the line is a single flat colour
	r    []float64 // line mean colour, 0..255
	g    []float64
	b    []float64
}

// lineProfiles samples every scan line at a fixed set of positions across it.
// Walking the axis swaps which pixel stride advances the profile: rows for a
// vertical stitch, columns for a horizontal one.
func lineProfiles(rgba *image.RGBA, origin image.Point, pts []int, ax axis.Axis, length int) profiles {
	n := len(pts)
	p := profiles{
		diff: make([]float64, length),
		flat: make([]float64, length),
		r:    make([]float64, length),
		g:    make([]float64, length),
		b:    make([]float64, length),
	}
	prevR := make([]uint8, n)
	prevG := make([]uint8, n)
	prevB := make([]uint8, n)
	pix := rgba.Pix
	stride := rgba.Stride

	lineStep, pointStep := stride, 4
	first := origin.Y*stride + origin.X*4
	if ax == axis.X {
		lineStep, pointStep = 4, stride
		first = origin.X*4 + origin.Y*stride
	}

	for i := 0; i < length; i++ {
		base := first + i*lineStep
		var sr, sg, sb uint64
		for _, q := range pts {
			o := base + q*pointStep
			sr += uint64(pix[o])
			sg += uint64(pix[o+1])
			sb += uint64(pix[o+2])
		}
		nn := uint64(n)
		mr, mg, mb := int(sr/nn), int(sg/nn), int(sb/nn)
		p.r[i], p.g[i], p.b[i] = float64(mr), float64(mg), float64(mb)

		var dev, dsum uint64
		for j, q := range pts {
			o := base + q*pointStep
			r, g, bl := int(pix[o]), int(pix[o+1]), int(pix[o+2])
			dev += uint64(absInt(r-mr) + absInt(g-mg) + absInt(bl-mb))
			if i > 0 {
				dsum += uint64(absInt(r-int(prevR[j])) + absInt(g-int(prevG[j])) + absInt(bl-int(prevB[j])))
			}
			prevR[j], prevG[j], prevB[j] = uint8(r), uint8(g), uint8(bl)
		}
		p.flat[i] = 1 - float64(dev)/float64(n)/maxL1
		if i > 0 {
			p.diff[i] = float64(dsum) / float64(n) / maxL1
		}
	}
	return p
}

// composition measures how different the picture looks on the two sides of a
// position. A real stitch joins two unrelated photos; a straight edge inside one
// photo only swaps a small part of the block.
func (p profiles) composition(i, block int) float64 {
	h := len(p.diff)
	// Too close to either end there is no full window on one side, and whatever
	// sits in the picture's corner — its own padding, a caption bar — would read
	// as a changed composition. No window, no claim.
	if i < block || h-i < block {
		return 0
	}
	ar, ag, ab := blockMean3(p.r, p.g, p.b, i-block, i)
	br, bg, bb := blockMean3(p.r, p.g, p.b, i, i+block)
	dist := (absF(ar-br) + absF(ag-bg) + absF(ab-bb)) / maxL1
	return clamp01(dist / 0.10)
}

func blockMean3(r, g, b []float64, lo, hi int) (float64, float64, float64) {
	var sr, sg, sb float64
	for y := lo; y < hi; y++ {
		sr += r[y]
		sg += g[y]
		sb += b[y]
	}
	n := float64(hi - lo)
	return sr / n, sg / n, sb / n
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func smooth(v []float64) {
	if len(v) < 3 {
		return
	}
	cp := make([]float64, len(v))
	copy(cp, v)
	for i := 1; i < len(v)-1; i++ {
		v[i] = (cp[i-1] + cp[i] + cp[i+1]) / 3
	}
}

// blockBaseline is the average activity of a neighbourhood around each line, so
// a busy photo does not out-shout the seam above it.
func blockBaseline(diff []float64, block int) []float64 {
	h := len(diff)
	block = clampInt(block, 16, h)
	nb := (h + block - 1) / block
	mean := make([]float64, nb)
	for i := 0; i < nb; i++ {
		lo := i * block
		hi := minInt(lo+block, h)
		var s float64
		for _, v := range diff[lo:hi] {
			s += v
		}
		mean[i] = s / float64(hi-lo)
	}
	out := make([]float64, h)
	for i := 0; i < h; i++ {
		b := i / block
		lo, hi := maxInt(b-1, 0), minInt(b+2, nb)
		var s float64
		for _, v := range mean[lo:hi] {
			s += v
		}
		out[i] = s / float64(hi-lo)
	}
	return out
}

func isLocalMax(v []float64, i, win int) bool {
	lo, hi := maxInt(i-win, 0), minInt(i+win, len(v)-1)
	for j := lo; j <= hi; j++ {
		if j != i && v[j] > v[i] {
			return false
		}
	}
	return true
}

// snapToFlatBand moves a position onto the nearest plain stretch, which is where
// stitched photos are usually separated. Padding qualifies only when it is at
// least two lines thick and calm inside — a photo's own straight edge is a
// single noisy line. It reports whether the cut ended up on real padding.
func snapToFlatBand(flat, diff []float64, pos, length, win int) (int, bool) {
	// Padding is near-identical pixels, so the bar sits well above what a textured
	// photo line reaches even with compression noise.
	const plain = 0.99
	best := -1
	for d := -win; d <= win; d++ {
		i := pos + d
		if i < 1 || i >= length-1 {
			continue
		}
		if flat[i] >= plain && (best < 0 || flat[i] > flat[best]) {
			best = i
		}
	}
	if best < 0 {
		return pos, false
	}
	lo, hi := best, best
	for lo-1 > 0 && flat[lo-1] >= plain {
		lo--
	}
	for hi+1 < length && flat[hi+1] >= plain {
		hi++
	}
	// A plain stretch this thick is sky or a wall inside one photo, not a seam.
	if thickness := hi - lo + 1; thickness < 3 || thickness > 240 {
		return pos, false
	}
	// Judge the padding by its own lines; the two boundary lines are the jump
	// that made this a peak in the first place.
	var activity float64
	for j := lo + 1; j < hi; j++ {
		activity += diff[j]
	}
	if activity/float64(hi-lo-1) > 0.02 {
		return pos, false
	}
	return (lo + hi) / 2, true
}

func suppress(in []Candidate, minGap int) []Candidate {
	kept := make([]Candidate, 0, len(in))
	for _, c := range in {
		ok := true
		for _, k := range kept {
			if absInt(k.Pos-c.Pos) < minGap {
				ok = false
				break
			}
		}
		if ok {
			kept = append(kept, c)
		}
	}
	return kept
}

func buildSignal(diff, flat []float64, length int) Signal {
	step := maxInt(1, length/1000)
	lines := (length + step - 1) / step
	s := Signal{Step: step, Lines: lines, Diff: make([]float64, lines), Flat: make([]float64, lines)}
	for i := 0; i < lines; i++ {
		lo := i * step
		hi := minInt(lo+step, length)
		var dmax, fsum float64
		for j := lo; j < hi; j++ {
			if diff[j] > dmax {
				dmax = diff[j]
			}
			fsum += flat[j]
		}
		s.Diff[i] = round3(math.Min(dmax/0.30, 1))
		s.Flat[i] = round3(fsum / float64(hi-lo))
	}
	return s
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clampInt(v, lo, hi int) int { return minInt(maxInt(v, lo), hi) }
func minInt(a, b int) int        { return min(a, b) }
func maxInt(a, b int) int        { return max(a, b) }

func clamp01(v float64) float64 { return math.Min(1, math.Max(0, v)) }

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
