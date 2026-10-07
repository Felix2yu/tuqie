package preview

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestFit(t *testing.T) {
	for _, tc := range []struct {
		inW, inH, wantW, wantH int
		needed                 bool
	}{
		{1080, 3619, 1080, 3619, false},
		{3000, 3000, 2000, 2000, true},
		{1080, 111000, 80, 8192, true},
		{1, 20000, 1, 8192, true},
		{0, 100, 0, 100, false},
	} {
		w, h, needed := Fit(tc.inW, tc.inH)
		if w != tc.wantW || h != tc.wantH || needed != tc.needed {
			t.Errorf("Fit(%d,%d) = %d,%d,%v, want %d,%d,%v", tc.inW, tc.inH, w, h, needed, tc.wantW, tc.wantH, tc.needed)
		}
	}
}

// decodePreview renders src at w x h and reads the JPEG back.
func decodePreview(t *testing.T, src *image.RGBA, w, h int) image.Image {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, src, w, h); err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if r := img.Bounds(); r.Dx() != w || r.Dy() != h {
		t.Fatalf("preview is %v, want %dx%d", r, w, h)
	}
	return img
}

func TestEncodeAveragesEachBox(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	src.SetRGBA(1, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	src.SetRGBA(0, 1, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	src.SetRGBA(1, 1, color.RGBA{R: 0, G: 0, B: 255, A: 255})

	img := decodePreview(t, src, 1, 1)
	r, g, b, _ := img.At(0, 0).RGBA()
	// Every channel averages a quarter of the way to 255; JPEG is lossy, so the
	// check is that the box was averaged rather than sampled.
	if d := abs(int(r>>8) - 64); d > 4 {
		t.Errorf("red = %d, want about 64", r>>8)
	}
	if d := abs(int(g>>8) - 64); d > 4 {
		t.Errorf("green = %d, want about 64", g>>8)
	}
	if d := abs(int(b>>8) - 64); d > 4 {
		t.Errorf("blue = %d, want about 64", b>>8)
	}
}

func TestEncodeFlattensAlphaOnThePage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4)) // nothing but transparent black
	img := decodePreview(t, src, 2, 2)
	r, g, b, _ := img.At(1, 1).RGBA()
	for _, tc := range []struct {
		name string
		got  int
		want int
	}{{"red", int(r >> 8), backdropR}, {"green", int(g >> 8), backdropG}, {"blue", int(b >> 8), backdropB}} {
		if abs(tc.got-tc.want) > 4 {
			t.Errorf("%s = %d, want the backdrop %d", tc.name, tc.got, tc.want)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
