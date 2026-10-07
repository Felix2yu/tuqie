package store

import (
	"os"
	"strings"
	"tuqie/internal/exif"

	// Each of these registers a decoder with image.Decode when imported, and each
	// keeps its EXIF inside a container the stdlib TIFF walk never reaches: an
	// ISO-BMFF item for HEIC and AVIF, a box in the JXL codestream.
	"github.com/gen2brain/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/jxl"
)

// metaFor reports the capture date and orientation an upload declares. PNG and
// JPEG are read from the bytes already held for the probe; the container formats
// seek to their own metadata box in the file on disk, so their cost does not
// grow with the upload. A file none of them can parse says nothing: readers take
// the missing orientation as upright, and the uploader's file timestamp stands in
// for the date.
func metaFor(format, path string, head []byte) exif.Doc {
	switch format {
	case "heic", "avif", "jxl":
		return containerMeta(format, path)
	}
	return exif.Meta(head)
}

func containerMeta(format, path string) exif.Doc {
	f, err := os.Open(path)
	if err != nil {
		return exif.Doc{}
	}
	defer f.Close()

	switch format {
	case "heic":
		if e, err := heic.DecodeExif(f); err == nil {
			return doc(e.Orientation, e.DateTimeOriginal, e.DateTime)
		}
	case "avif":
		if e, err := avif.DecodeExif(f); err == nil {
			return doc(e.Orientation, e.DateTimeOriginal, e.DateTime)
		}
	case "jxl":
		if e, err := jxl.DecodeExif(f); err == nil {
			return doc(e.Orientation, e.DateTimeOriginal, e.DateTime)
		}
	}
	return exif.Doc{}
}

// doc narrows what three independently written EXIF readers report down to what
// the store keeps. Orientation outside the eight EXIF defines is dropped rather
// than guessed at, and a date only counts if it parses.
func doc(orientation int, dates ...string) exif.Doc {
	var d exif.Doc
	if orientation >= 1 && orientation <= 8 {
		d.Orientation = uint16(orientation)
	}
	for _, s := range dates {
		candidate := exif.Date{Wall: strings.TrimSpace(s)}
		if candidate.Valid() {
			d.Taken = candidate
			break
		}
	}
	return d
}
