package split

import (
	"bytes"
	"encoding/binary"
	"image"
	"io"

	"tuqie/internal/exif"

	"github.com/gen2brain/jxl"
)

// This file gives the two container encoders what they cannot do themselves:
// jxl.Encode and avif.Encode take no metadata, so the capture date is written in
// after the pixels are coded, in the shape the reference tools use.

// isoBox is an ISO-BMFF box with a 32-bit size, which every box written here fits in.
func isoBox(name string, body ...[]byte) []byte {
	size := 8
	for _, part := range body {
		size += len(part)
	}
	var out bytes.Buffer
	var head [8]byte
	binary.BigEndian.PutUint32(head[:4], uint32(size))
	copy(head[4:], name)
	out.Write(head[:])
	for _, part := range body {
		out.Write(part)
	}
	return out.Bytes()
}

func be32(v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return b[:]
}

// jxlSlice writes a picture as a JPEG XL container: a bare codestream has no
// agreed place for metadata — libjxl itself drops the date when it is asked to
// write one — so the codestream goes into "JXL " + ftyp + Exif + jxlc, the box
// order cjxl produces.
func jxlSlice(w io.Writer, img image.Image, quality int, taken exif.Date) error {
	var code bytes.Buffer
	if err := jxl.Encode(&code, img, jxl.EncodeOptions{Quality: quality, Effort: jxlEffort}); err != nil {
		return err
	}

	out := isoBox("JXL ", []byte{0x0d, 0x0a, 0x87, 0x0a})
	out = append(out, isoBox("ftyp", []byte("jxl "), be32(0), []byte("jxl "))...)
	if tiff := taken.TIFF(); len(tiff) > 0 {
		// The four leading bytes are the offset from the start of the box body to
		// the TIFF header, which is right after them.
		out = append(out, isoBox("Exif", append(be32(0), tiff...))...)
	}
	out = append(out, isoBox("jxlc", code.Bytes())...)

	_, err := w.Write(out)
	return err
}
