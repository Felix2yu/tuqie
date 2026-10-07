// Command gensample writes a synthetic stitched screenshot for manual testing.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"math/rand"
	"os"
)

// options mirrors the command's flags so generation stays testable.
type options struct {
	out    string
	width  int
	photos int
	gap    int
	seed   int64
	axis   string
}

func main() {
	var opt options
	flag.StringVar(&opt.out, "out", "/tmp/tuqie-sample.png", "output path")
	flag.IntVar(&opt.width, "width", 1080, "image width")
	flag.IntVar(&opt.photos, "photos", 4, "how many photos are stitched together")
	flag.IntVar(&opt.gap, "gap", 18, "blank band between photos")
	flag.Int64Var(&opt.seed, "seed", 2026, "random seed")
	flag.StringVar(&opt.axis, "axis", "y", "stitch direction: y stacks downward, x runs left to right")
	flag.Parse()

	if err := generate(opt, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func generate(opt options, report io.Writer) error {
	rng := rand.New(rand.NewSource(opt.seed))
	heights := make([]int, opt.photos)
	total := opt.photos * opt.gap
	for i := range heights {
		heights[i] = 700 + rng.Intn(600)
		total += heights[i]
	}

	img := image.NewRGBA(image.Rect(0, 0, opt.width, total))
	y := 0
	for _, h := range heights {
		drawPhoto(img, opt.width, y, h, rng)
		y += h
		for dy := 0; dy < opt.gap; dy++ {
			for x := 0; x < opt.width; x++ {
				img.SetRGBA(x, y, color.RGBA{246, 246, 247, 255})
			}
			y++
		}
	}

	horizontal := opt.axis == "x"
	if horizontal {
		img = transpose(img)
	}

	f, err := os.Create(opt.out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	w, h := opt.width, total
	if horizontal {
		w, h = total, opt.width
	}
	fmt.Fprintf(report, "%s: %dx%d, %d photos, axis=%s, seams at %v\n", opt.out, w, h, opt.photos, opt.axis, seams(heights, opt.gap))
	return nil
}

// transpose turns a vertical stack into a left-to-right one.
func transpose(src *image.RGBA) *image.RGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.SetRGBA(y, x, src.RGBAAt(x, y))
		}
	}
	return dst
}

// drawPhoto fills one band with recognisable content: a sky gradient, a horizon,
// circles, and rows of bars that look like text lines.
func drawPhoto(img *image.RGBA, width, top, height int, rng *rand.Rand) {
	base := color.RGBA{R: uint8(40 + rng.Intn(180)), G: uint8(40 + rng.Intn(180)), B: uint8(40 + rng.Intn(180)), A: 255}
	for y := 0; y < height; y++ {
		t := float64(y) / float64(height)
		// Soft horizon: real photos rarely have a full-width hard edge mid-frame.
		ground := 0.0
		if t > 0.62 {
			ground = (t - 0.62) / 0.10
			if ground > 1 {
				ground = 1
			}
		}
		for x := 0; x < width; x++ {
			// Per-pixel grain, so interior rows are as textured as a real photo
			// instead of looking like flat padding.
			grain := ((x * 73856093) ^ (y * 19349663)) % 44
			sky := color.RGBA{
				R: uint8(int(float64(base.R)*(1-t*0.6)) + x*8/width + grain),
				G: uint8(int(float64(base.G)*(1-t*0.4)) + y*4/height + grain/2),
				B: uint8(int(float64(base.B)*(1-t*0.2)) + grain/3),
				A: 255,
			}
			soil := color.RGBA{uint8(30 + (x*3)%120), uint8(60 + grain), uint8(40 + (y*5)%90), 255}
			img.SetRGBA(x, top+y, blend(sky, soil, ground))
		}
	}
	for i := 0; i < 4; i++ {
		// Content stays inside its own photo; nothing paints over the padding.
		cv := color.RGBA{uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256)), 255}
		r := min(40+rng.Intn(80), width/2)
		circle(img, image.Rect(0, top, width, top+height), r+rng.Intn(max(1, width-2*r)), top+rng.Intn(height), r, cv)
	}
	for row := 0; row < 3; row++ {
		bandY := top + 20 + row*40
		for x := 24; x < width-24-rng.Intn(300); x++ {
			img.SetRGBA(x, bandY, blend(img.RGBAAt(x, bandY), color.RGBA{250, 250, 250, 255}, 0.35))
		}
	}
}

func blend(a, b color.RGBA, t float64) color.RGBA {
	mix := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: 255}
}

func circle(img *image.RGBA, clip image.Rectangle, cx, cy, r int, c color.RGBA) {
	for y := cy - r; y <= cy+r; y++ {
		if y < clip.Min.Y || y >= clip.Max.Y {
			continue
		}
		for x := cx - r; x <= cx+r; x++ {
			if x < clip.Min.X || x >= clip.Max.X {
				continue
			}
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func seams(heights []int, gap int) []int {
	var out []int
	y := 0
	for i, h := range heights {
		y += h
		if i < len(heights)-1 {
			out = append(out, y+gap/2)
		}
		y += gap
	}
	return out
}
