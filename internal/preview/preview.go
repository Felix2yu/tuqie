// Package preview renders an upload small enough for a browser to keep on
// screen. A stitched screenshot can run to 120 megapixels, which is several
// hundred megabytes once decoded, and mobile browsers refuse to draw images that
// large at all, so the working view gets a downscaled copy and the slices stay
// full resolution.
package preview

import (
	"image"
	"image/jpeg"
	"io"
	"math"
)

const (
	// maxPixels is what a phone still decodes without giving up; iOS Safari
	// rejects canvases and images past roughly this size.
	maxPixels = 4_000_000
	// maxSide is the longer edge a browser clips textures at.
	maxSide = 8_192
	quality = 85
	// backdrop is the page colour the browser would have composited a
	// transparent upload against anyway.
	backdropR, backdropG, backdropB = 8, 11, 16
)

// Fit is the size a preview of a width x height picture ends up being, and
// whether one is worth making: an upload that already fits the budget is served
// as it is.
func Fit(width, height int) (w, h int, needed bool) {
	if width <= 0 || height <= 0 {
		return width, height, false
	}
	scale := 1.0
	if total := float64(width) * float64(height); total > maxPixels {
		scale = math.Sqrt(maxPixels / total)
	}
	if long := math.Max(float64(width), float64(height)); long > maxSide {
		scale = math.Min(scale, maxSide/long)
	}
	if scale >= 1 {
		return width, height, false
	}
	return atLeastOne(int(float64(width)*scale + 0.5)), atLeastOne(int(float64(height)*scale + 0.5)), true
}

// Encode writes a box-filtered JPEG of src at width x height. Every destination
// pixel is the average of the source square it stands for, so thin lines and
// text survive the shrink instead of aliasing away.
func Encode(w io.Writer, src *image.RGBA, width, height int) error {
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	for oy := 0; oy < height; oy++ {
		y0, y1 := span(oy, height, sh)
		for ox := 0; ox < width; ox++ {
			x0, x1 := span(ox, width, sw)
			var sr, sg, sb, sa, n int64
			for y := y0; y < y1; y++ {
				base := y*src.Stride + x0*4
				for x := base; x < base+(x1-x0)*4; x += 4 {
					sr += int64(src.Pix[x])
					sg += int64(src.Pix[x+1])
					sb += int64(src.Pix[x+2])
					sa += int64(src.Pix[x+3])
					n++
				}
			}
			// Go stores RGBA alpha-premultiplied, and compositing over an opaque
			// background is linear in those values, so averaging them first and
			// flattening after gives the same result as flattening every pixel.
			alpha := sa / n
			d := oy*out.Stride + ox*4
			out.Pix[d] = uint8(sr/n + (255-alpha)*int64(backdropR)/255)
			out.Pix[d+1] = uint8(sg/n + (255-alpha)*int64(backdropG)/255)
			out.Pix[d+2] = uint8(sb/n + (255-alpha)*int64(backdropB)/255)
			out.Pix[d+3] = 255
		}
	}
	return jpeg.Encode(w, out, &jpeg.Options{Quality: quality})
}

// span is the run of source cells one destination row or column stands for.
func span(i, dst, src int) (int, int) {
	lo := i * src / dst
	hi := (i + 1) * src / dst
	if hi <= lo {
		return lo, lo + 1
	}
	return lo, hi
}

func atLeastOne(v int) int {
	if v < 1 {
		return 1
	}
	return v
}
