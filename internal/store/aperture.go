package store

import (
	"encoding/binary"
	"image"
	"io"
	"os"
)

// display is the geometry an ISO-BMFF picture item asks for: which part of the
// coded samples is picture, and how the container wants it turned. Encoders
// size a photo up to the codec's block grid, so the decoded buffer is commonly
// a row or two taller than what every viewer shows; the clean aperture names
// the difference.
type display struct {
	// crop is the picture inside the decoded buffer. The zero rectangle means
	// the file asks for none of it to be dropped.
	crop image.Rectangle
	// orientation is the EXIF tag the container's own rotation implies. 0 means
	// the file says nothing, which readers take as upright.
	orientation uint16
}

// property is one entry of an item property container, kept in the order the
// file lists them because the association box numbers them from one.
type property struct {
	kind string
	body []byte
}

// maxMeta bounds the metadata this reader holds in memory. Real uploads keep a
// few kilobytes there, so a larger claim is not metadata worth trusting.
const maxMeta = 1 << 22

// displayFor reads the display geometry of a HEIC or AVIF upload. It walks only
// the header, so its cost does not grow with the picture, and reports nothing
// for a file it cannot parse; the upload then keeps the coded size and the EXIF
// tag the probe already found.
func displayFor(format, path string) display {
	if format != "heic" && format != "avif" {
		return display{}
	}
	f, err := os.Open(path)
	if err != nil {
		return display{}
	}
	defer f.Close()

	meta, err := boxPayload(f, "meta")
	if err != nil {
		return display{}
	}
	return metaDisplay(meta)
}

// boxPayload scans the top level of an ISO-BMFF file for a box and returns what
// is inside it.
func boxPayload(r io.ReadSeeker, want string) ([]byte, error) {
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			return nil, err
		}
		name := string(hdr[4:8])
		size := int64(binary.BigEndian.Uint32(hdr[:4]))
		head := int64(8)
		if size == 1 {
			var wide [8]byte
			if _, err := io.ReadFull(r, wide[:]); err != nil {
				return nil, err
			}
			size = int64(binary.BigEndian.Uint64(wide[:]))
			head = 16
		}
		// A header shorter than its own size field, or one claiming the rest of
		// the file, leaves nothing past it to look for.
		if size < head {
			return nil, io.ErrUnexpectedEOF
		}
		body := size - head
		if name != want {
			// Boxes are skipped without being read, so a pixel payload ahead of
			// the metadata costs one seek.
			if _, err := r.Seek(body, io.SeekCurrent); err != nil {
				return nil, err
			}
			continue
		}
		if body > maxMeta {
			return nil, io.ErrUnexpectedEOF
		}
		payload := make([]byte, body)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		return payload, nil
	}
}

// metaDisplay picks the primary item out of a meta box and reads the display
// properties associated with it.
func metaDisplay(meta []byte) display {
	if len(meta) < 4 {
		return display{}
	}
	// meta is a fullbox: version and flags precede its children.
	var (
		primary uint32
		props   []property
		ipma    []byte
	)
	eachBox(meta[4:], func(name string, body []byte) {
		switch name {
		case "pitm":
			primary = pitmID(body)
		case "iprp":
			eachBox(body, func(name string, inner []byte) {
				switch name {
				case "ipco":
					eachBox(inner, func(kind string, prop []byte) {
						props = append(props, property{kind: kind, body: prop})
					})
				case "ipma":
					ipma = inner
				}
			})
		}
	})
	assocs := ipmaItems(ipma, primary)
	if primary == 0 || assocs == nil {
		return display{}
	}

	var d display
	ispeW, ispeH, coded := 0, 0, false
	var clap [8]uint32
	cropped := false
	rotated, mirrored := -1, false
	for _, index := range assocs {
		if index < 1 || int(index) > len(props) {
			continue
		}
		p := props[index-1]
		body := p.body
		switch p.kind {
		case "ispe":
			// A fullbox: the two sizes follow its version and flags.
			if len(body) >= 12 {
				ispeW, ispeH, coded = int(binary.BigEndian.Uint32(body[4:8])), int(binary.BigEndian.Uint32(body[8:12])), true
			}
		case "clap":
			if len(body) >= 32 {
				for i := range 8 {
					clap[i] = binary.BigEndian.Uint32(body[i*4 : i*4+4])
				}
				cropped = true
			}
		case "irot":
			if len(body) >= 1 {
				rotated = int(body[0]) & 3
			}
		case "imir":
			mirrored = true
		}
	}

	if coded && cropped {
		if rect, ok := cleanAperture(&clap, ispeW, ispeH); ok {
			d.crop = rect
		}
	}
	// A mirrored upload is not a case the four EXIF quarter turns describe, and
	// nothing that takes a photo writes one, so its turn is left to the EXIF tag.
	if rotated >= 0 && !mirrored {
		d.orientation = []uint16{1, 8, 3, 6}[rotated]
	}
	return d
}

// pitmID is the item a file marks as the one to show.
func pitmID(body []byte) uint32 {
	if len(body) < 6 {
		return 0
	}
	if body[0] == 0 {
		return uint32(binary.BigEndian.Uint16(body[4:6]))
	}
	if len(body) < 8 {
		return 0
	}
	return binary.BigEndian.Uint32(body[4:8])
}

// ipmaItems returns the property indexes associated with want, or nil when the
// association box does not name that item at all.
func ipmaItems(body []byte, want uint32) []uint32 {
	if len(body) < 8 {
		return nil
	}
	version, flags := body[0], body[3]
	count := int(binary.BigEndian.Uint32(body[4:8]))
	if count > len(body) {
		return nil
	}
	pos := 8
	for range count {
		var id uint32
		if version < 1 {
			if pos+2 > len(body) {
				return nil
			}
			id = uint32(binary.BigEndian.Uint16(body[pos : pos+2]))
			pos += 2
		} else {
			if pos+4 > len(body) {
				return nil
			}
			id = binary.BigEndian.Uint32(body[pos : pos+4])
			pos += 4
		}
		if pos+1 > len(body) {
			return nil
		}
		n := int(body[pos])
		pos++
		var indexes []uint32
		for range n {
			var index uint32
			if flags&1 != 0 {
				if pos+2 > len(body) {
					return nil
				}
				index = uint32(binary.BigEndian.Uint16(body[pos:pos+2]) & 0x7fff)
				pos += 2
			} else {
				if pos+1 > len(body) {
					return nil
				}
				index = uint32(body[pos] & 0x7f)
				pos++
			}
			indexes = append(indexes, index)
		}
		if id == want {
			return indexes
		}
	}
	return nil
}

// cleanAperture is the rectangle a clap box selects inside a coded picture. The
// box states size and offset as fractions, and places the aperture by its
// centre, so the arithmetic stays in fractions until it has to be a whole pixel.
func cleanAperture(clap *[8]uint32, w, h int) (image.Rectangle, bool) {
	widthN, widthD := int64(int32(clap[0])), int64(int32(clap[1]))
	heightN, heightD := int64(int32(clap[2])), int64(int32(clap[3]))
	horizN, horizD := int64(int32(clap[4])), int64(int32(clap[5]))
	vertN, vertD := int64(int32(clap[6])), int64(int32(clap[7]))
	if widthD <= 0 || heightD <= 0 || horizD <= 0 || vertD <= 0 || widthN < 0 || heightN < 0 {
		return image.Rectangle{}, false
	}
	if widthN%widthD != 0 || heightN%heightD != 0 {
		return image.Rectangle{}, false
	}
	cw, ch := widthN/widthD, heightN/heightD
	numX := int64(w)*horizD + 2*horizN - cw*horizD
	numY := int64(h)*vertD + 2*vertN - ch*vertD
	if numX%(2*horizD) != 0 || numY%(2*vertD) != 0 {
		return image.Rectangle{}, false
	}
	x, y := numX/(2*horizD), numY/(2*vertD)
	if x < 0 || y < 0 || cw <= 0 || ch <= 0 || x+cw > int64(w) || y+ch > int64(h) {
		return image.Rectangle{}, false
	}
	return image.Rect(int(x), int(y), int(x+cw), int(y+ch)), true
}

// eachBox walks the children of a container box, handing each one its type and
// what is inside it.
func eachBox(data []byte, fn func(name string, body []byte)) {
	for len(data) >= 8 {
		size := int(binary.BigEndian.Uint32(data[:4]))
		name := string(data[4:8])
		head := 8
		if size == 1 {
			if len(data) < 16 {
				return
			}
			wide := int(binary.BigEndian.Uint64(data[8:16]))
			if wide < 16 || wide > len(data) {
				return
			}
			size, head = wide, 16
		}
		if size == 0 {
			fn(name, data[head:])
			return
		}
		if size < head || size > len(data) {
			return
		}
		fn(name, data[head:size])
		data = data[size:]
	}
}
