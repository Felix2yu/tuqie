// Package split crops bands out of a long screenshot and encodes them.
package split

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"tuqie/internal/axis"
	"tuqie/internal/exif"

	// The three container formats are encoded by decoders' siblings: pure Go
	// HEVC, JPEG XL, and an AV1 encoder compiled to WASM that wazero runs.
	"github.com/gen2brain/avif"
	"github.com/gen2brain/h265/heic"
)

const (
	FormatPNG  = "png"
	FormatJPEG = "jpeg"
	FormatHEIC = "heic"
	FormatAVIF = "avif"
	FormatJXL  = "jxl"
)

// Encoding knobs the containers do not name themselves. AVIF's Speed is left
// explicit because that encoder treats a zero as its slowest preset rather than
// as "use a default", and JXL's effort buys a few percent of size at a cost a
// phone screenshot does not warrant.
const (
	avifSpeed   = 8
	jxlEffort   = 4
	defaultQual = 92
)

// avifSlots bounds how many WASM AVIF encodes run at once. One peaks near 600 MB
// of process memory and wazero does not hand it back, so two exports in flight
// would double that on the small machine this binary is usually dropped onto.
var avifSlots = make(chan struct{}, 1)

// Slice returns the band [from, to) along ax of src as a new RGBA image. src must
// have its origin at (0,0) — store guarantees that. Cut positions come from the
// client, so they are clamped rather than trusted.
func Slice(src *image.RGBA, ax axis.Axis, from, to int) (*image.RGBA, error) {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	length := ax.Length(w, h)
	lo := clamp(from, 0, length)
	hi := clamp(to, lo, length)
	if hi-lo < 1 {
		return nil, fmt.Errorf("empty slice %d..%d", from, to)
	}
	if ax == axis.X {
		out := image.NewRGBA(image.Rect(0, 0, hi-lo, h))
		for y := 0; y < h; y++ {
			s := y*src.Stride + lo*4
			d := y * out.Stride
			copy(out.Pix[d:d+(hi-lo)*4], src.Pix[s:s+(hi-lo)*4])
		}
		return out, nil
	}
	out := image.NewRGBA(image.Rect(0, 0, w, hi-lo))
	for y := 0; y < hi-lo; y++ {
		s := (y + lo) * src.Stride
		d := y * out.Stride
		copy(out.Pix[d:d+w*4], src.Pix[s:s+w*4])
	}
	return out, nil
}

// Encode writes img in the requested format. taken is the capture date of the
// source image: every output that can carry metadata does, so a saved slice keeps
// the original timestamp instead of the moment it was cut. quality is the lossy
// knob JPEG, HEIC, AVIF and JXL share; PNG stays lossless and ignores it.
func Encode(w io.Writer, img image.Image, format string, quality int, taken exif.Date) error {
	switch format {
	case FormatJPEG:
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: lossy(quality)}); err != nil {
			return err
		}
		_, err := w.Write(exif.InjectJPEG(buf.Bytes(), taken.App1()))
		return err
	case FormatPNG:
		// eXIf has to sit between IHDR and IDAT, so the encoder runs into a buffer
		// first; a band is a slice of the source, not the whole image.
		var buf bytes.Buffer
		enc := &png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, img); err != nil {
			return err
		}
		_, err := w.Write(exif.InjectPNG(buf.Bytes(), taken.PngChunk()))
		return err
	case FormatHEIC:
		// Of the three container encoders only this one takes metadata, so a HEIC
		// slice carries the date the way a JPEG one does.
		return heic.Encode(w, img, heic.EncodeOptions{
			Quality: encoderQuality(FormatHEIC, quality),
			Chroma:  heicChroma(img),
			Exif:    taken.TIFF(),
		})
	case FormatAVIF:
		avifSlots <- struct{}{}
		defer func() { <-avifSlots }()
		var buf bytes.Buffer
		if err := avif.Encode(&buf, img, avif.Options{Quality: encoderQuality(FormatAVIF, quality), Speed: avifSpeed}); err != nil {
			return err
		}
		out := buf.Bytes()
		if tiff := taken.TIFF(); len(tiff) > 0 {
			out = avifWithDate(out, tiff)
		}
		_, err := w.Write(out)
		return err
	case FormatJXL:
		return jxlSlice(w, img, encoderQuality(FormatJXL, quality), taken)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

// sliderStops are the slider positions qualityCurve is tabulated at.
var sliderStops = []int{60, 70, 80, 90, 100}

// qualityCurve maps the 画质 slider onto each encoder's own scale, because those
// scales are not comparable: fed the slider straight in, a HEIC slice at 90 costs
// about twice what a JPEG at 90 costs for a picture that looks the same. Each row
// is the quality at which that encoder reaches, on a real photo and a real
// screenshot averaged, the PSNR JPEG hits at the same slider position — so one
// position means one look whatever the container.
var qualityCurve = map[string][]int{
	FormatHEIC: {38, 44, 52, 60, 83},
	FormatAVIF: {38, 45, 56, 76, 90},
	FormatJXL:  {72, 76, 83, 89, 96},
}

// encoderQuality is the quality a format should be coded at for the slider value
// the user left where it is. JPEG and PNG keep taking the number as it stands.
func encoderQuality(format string, slider int) int {
	curve, ok := qualityCurve[format]
	if !ok {
		return lossy(slider)
	}
	q := clamp(slider, sliderStops[0], sliderStops[len(sliderStops)-1])
	for i := 1; i < len(sliderStops); i++ {
		if q <= sliderStops[i] {
			lo, hi := sliderStops[i-1], sliderStops[i]
			return curve[i-1] + (curve[i]-curve[i-1])*(q-lo)/(hi-lo)
		}
	}
	return curve[len(curve)-1]
}

// lossy is the quality a lossy format is asked for, with the range the slider
// cannot guarantee but a hand-built request can.
func lossy(quality int) int {
	if quality <= 0 || quality > 100 {
		return defaultQual
	}
	return quality
}

// heicChroma is the sampling a HEIC slice can be coded at. 4:2:0 needs both sides
// even: the encoder rounds the coded picture up to the subsampling grid but signals
// the odd display size in ispe without a conformance window, and libheif — iOS
// Photos along with it — then refuses the file for decoding to the wrong size.
// Roughly half the bands a user cuts have an odd height, so those go out as 4:4:4,
// which codes any size for a couple of tens of percent more bytes.
func heicChroma(img image.Image) heic.ChromaFormat {
	b := img.Bounds()
	if b.Dx()%2 == 0 && b.Dy()%2 == 0 {
		return heic.Chroma420
	}
	return heic.Chroma444
}

// Ext is the file name suffix a format is saved under.
func Ext(format string) string {
	switch format {
	case FormatPNG:
		return "png"
	case FormatHEIC:
		return "heic"
	case FormatAVIF:
		return "avif"
	case FormatJXL:
		return "jxl"
	}
	return "jpg"
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
