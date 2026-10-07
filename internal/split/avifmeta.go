package split

import (
	"bytes"
	"encoding/binary"
)

// This file gives an AVIF file the capture date its encoder cannot: the TIFF
// payload is appended to mdat, and the item that points at it is written into
// iinf, iloc and iref, which is what MIAF asks of a file carrying Exif and what
// libavif, libheif and ImageIO go looking for.
//
// The rewrite is deliberately narrow. A box that does not look like the encoder's
// own output ends the attempt, because a wrong guess here costs the picture
// rather than the date.

// box is an ISO-BMFF box, kept with the bytes it was read from.
type box struct {
	name string
	body []byte
	full []byte
}

func splitBoxes(data []byte) []box {
	var out []box
	for len(data) >= 8 {
		size := int(binary.BigEndian.Uint32(data[:4]))
		head := 8
		if size == 1 {
			if len(data) < 16 {
				return out
			}
			size = int(binary.BigEndian.Uint64(data[8:16]))
			head = 16
		}
		if size == 0 {
			size = len(data)
		}
		if size < head || size > len(data) {
			return out
		}
		out = append(out, box{name: string(data[4:8]), body: data[head:size], full: data[:size]})
		data = data[size:]
	}
	return out
}

// avifWithDate returns file with an Exif item added, or file itself when its
// boxes are not the shape this rewrite understands.
func avifWithDate(file, tiff []byte) []byte {
	boxes := splitBoxes(file)
	metaAt, mdatAt := -1, -1
	for i, b := range boxes {
		switch b.name {
		case "meta":
			if metaAt >= 0 || len(b.body) < 4 {
				return file
			}
			metaAt = i
		case "mdat":
			if mdatAt < 0 {
				mdatAt = i
			}
		}
	}
	if metaAt < 0 || mdatAt != len(boxes)-1 || mdatAt < metaAt {
		return file
	}

	var (
		head     = boxes[metaAt].body[:4]
		children = splitBoxes(boxes[metaAt].body[4:])
		primary  uint32
		items    []ilocItem
		loc      ilocHeader
	)
	for _, c := range children {
		switch c.name {
		case "pitm":
			if len(c.body) >= 6 {
				primary = uint32(binary.BigEndian.Uint16(c.body[4:6]))
			}
		case "iloc":
			var ok bool
			if items, loc, ok = readIloc(c.body); !ok {
				return file
			}
		case "iinf":
			if len(c.body) < 6 {
				return file
			}
		case "iref":
			// Rewriting a reference list we did not write is a bigger guess than
			// the date is worth.
			return file
		case "idat":
			return file
		}
	}
	if primary == 0 || items == nil {
		return file
	}

	exifID := uint32(1)
	for _, it := range items {
		if it.id == 0 || it.id >= exifID {
			exifID = it.id + 1
		}
	}

	payload := append(be32(0), tiff...)
	mdatStart := 8
	for _, b := range boxes[:mdatAt] {
		mdatStart += len(b.full)
	}
	mdatLen := len(boxes[mdatAt].body)

	// The first pass leaves the shift at zero, which is enough to measure how much
	// bigger meta grows. The field widths do not change between passes, so the
	// second one lands on the real offsets.
	_, firstMeta, ok := buildAvif(boxes, metaAt, mdatAt, head, children, primary, items, loc, exifID, payload, 0, mdatStart, mdatLen)
	if !ok {
		return file
	}
	// Only the growth of meta moves the picture's bytes: the payload itself lands
	// after them.
	out, _, ok := buildAvif(boxes, metaAt, mdatAt, head, children, primary, items, loc, exifID, payload,
		firstMeta-len(boxes[metaAt].full), mdatStart, mdatLen)
	if !ok {
		return file
	}
	return out
}

// buildAvif reassembles the file with the payload appended to mdat, whose bytes
// start at mdatStart and run for mdatLen before meta grew by shift.
func buildAvif(boxes []box, metaAt, mdatAt int, head []byte, children []box, primary uint32, items []ilocItem, loc ilocHeader, exifID uint32, payload []byte, shift, mdatStart, mdatLen int) (out []byte, metaLen int, ok bool) {
	rebuilt := make([][]byte, 0, len(children)+1)
	ilocDone, iinfDone := false, false
	for _, c := range children {
		switch c.name {
		case "iloc":
			shifted := make([]ilocItem, 0, len(items)+1)
			for _, it := range items {
				if it.id == exifID {
					return nil, 0, false
				}
				if it.method == 0 {
					it.pos += uint64(shift)
				}
				shifted = append(shifted, it)
			}
			shifted = append(shifted, ilocItem{
				id:     exifID,
				pos:    uint64(mdatStart + shift + mdatLen),
				length: uint64(len(payload)),
			})
			rebuilt = append(rebuilt, isoBox("iloc", writeIloc(loc, shifted)))
			ilocDone = true
		case "iinf":
			rebuilt = append(rebuilt, isoBox("iinf", writeIinf(c.body, exifID)))
			iinfDone = true
		default:
			rebuilt = append(rebuilt, c.full)
		}
	}
	if !ilocDone || !iinfDone {
		return nil, 0, false
	}

	// libavif reads iref as a fullbox followed by its reference boxes with no
	// count in between, and writes them that way too; the count the spec puts
	// there makes it read the next box header out of place and reject the file.
	rebuilt = append(rebuilt, isoBox("iref", append([]byte{0, 0, 0, 0},
		isoBox("cdsc", be16(uint16(exifID)), be16(1), be16(uint16(primary)))...)))

	var meta bytes.Buffer
	meta.Write(head)
	for _, b := range rebuilt {
		meta.Write(b)
	}

	var buf bytes.Buffer
	metaBox := isoBox("meta", meta.Bytes())
	for i, b := range boxes {
		switch i {
		case metaAt:
			buf.Write(metaBox)
		case mdatAt:
			buf.Write(isoBox("mdat", append(append([]byte{}, b.body...), payload...)))
		default:
			buf.Write(b.full)
		}
	}
	return buf.Bytes(), len(metaBox), true
}

// ilocItem is one item's placement. The file offset is kept as one number
// because iloc splits it between a base and an extent offset, and which of the
// two carries the position depends on the field widths the encoder chose.
type ilocItem struct {
	id     uint32
	method int
	pos    uint64
	length uint64
}

// ilocHeader keeps the field widths the encoder chose, so the rewritten box is
// the same shape readers already parsed.
type ilocHeader struct {
	version    byte
	offsetSize int
	lengthSize int
	baseSize   int
	indexSize  int
}

func readIloc(body []byte) ([]ilocItem, ilocHeader, bool) {
	if len(body) < 8 {
		return nil, ilocHeader{}, false
	}
	loc := ilocHeader{
		version:    body[0],
		offsetSize: int(body[4] >> 4),
		lengthSize: int(body[4] & 0xf),
		baseSize:   int(body[5] >> 4),
	}
	if loc.version == 1 {
		loc.indexSize = int(body[5] & 0xf)
	} else if loc.version > 1 || loc.version != 0 && body[0] != 0 {
		return nil, loc, false
	}
	if !knownWidth(loc.offsetSize) || !knownWidth(loc.lengthSize) || !knownWidth(loc.baseSize) ||
		!knownWidth(loc.indexSize) {
		return nil, loc, false
	}

	pos := 6
	count := int(binary.BigEndian.Uint16(body[pos:]))
	pos += 2
	var items []ilocItem
	for i := 0; i < count; i++ {
		var it ilocItem
		if loc.version < 2 {
			if pos+2 > len(body) {
				return nil, loc, false
			}
			it.id = uint32(binary.BigEndian.Uint16(body[pos:]))
			pos += 2
		} else {
			it.id = binary.BigEndian.Uint32(body[pos:])
			pos += 4
		}
		if loc.version >= 1 {
			if pos+2 > len(body) {
				return nil, loc, false
			}
			it.method = int(binary.BigEndian.Uint16(body[pos:]) & 0xf)
			pos += 2
			if it.method != 0 {
				return nil, loc, false
			}
		}
		pos += 2 // data_reference_index
		var good bool
		base, good := takeUint(body, &pos, loc.baseSize)
		if !good {
			return nil, loc, false
		}
		if pos+2 > len(body) {
			return nil, loc, false
		}
		n := int(binary.BigEndian.Uint16(body[pos:]))
		pos += 2
		if n != 1 {
			return nil, loc, false
		}
		var off, length uint64
		if loc.version >= 1 && loc.indexSize > 0 {
			if _, good = takeUint(body, &pos, loc.indexSize); !good {
				return nil, loc, false
			}
		}
		if off, good = takeUint(body, &pos, loc.offsetSize); !good {
			return nil, loc, false
		}
		if length, good = takeUint(body, &pos, loc.lengthSize); !good {
			return nil, loc, false
		}
		it.pos, it.length = base+off, length
		items = append(items, it)
	}
	return items, loc, true
}

// writeIloc encodes the items back with the widths they were read with.
func writeIloc(loc ilocHeader, items []ilocItem) []byte {
	out := []byte{loc.version, 0, 0, 0, byte(loc.offsetSize<<4 | loc.lengthSize), byte(loc.baseSize<<4 | loc.indexSize)}
	out = append(out, be16(uint16(len(items)))...)
	for _, it := range items {
		if loc.version < 2 {
			out = append(out, be16(uint16(it.id))...)
		} else {
			out = append(out, be32(it.id)...)
		}
		if loc.version >= 1 {
			out = append(out, be16(uint16(it.method))...)
		}
		out = append(out, be16(0)...)
		base, off := uint64(0), it.pos
		if loc.baseSize > 0 {
			base, off = it.pos, 0
		}
		out = append(out, putUint(base, loc.baseSize)...)
		out = append(out, be16(1)...)
		out = append(out, putUint(off, loc.offsetSize)...)
		out = append(out, putUint(it.length, loc.lengthSize)...)
	}
	return out
}

// writeIinf copies the item info box and adds the Exif entry to it.
func writeIinf(body []byte, exifID uint32) []byte {
	out := append([]byte{}, body[:4]...)
	if body[0] == 0 {
		n := binary.BigEndian.Uint16(body[4:])
		out = append(out, be16(n+1)...)
		out = append(out, body[6:]...)
	} else {
		n := binary.BigEndian.Uint32(body[4:])
		out = append(out, be32(n+1)...)
		out = append(out, body[8:]...)
	}
	return append(out, isoBox("infe", []byte{2, 0, 0, 0}, be16(uint16(exifID)), be16(0), []byte("Exif"), []byte("exif\x00"))...)
}

func knownWidth(w int) bool { return w == 0 || w == 4 || w == 8 }

func takeUint(b []byte, pos *int, width int) (uint64, bool) {
	if *pos+width > len(b) {
		return 0, false
	}
	var v uint64
	for i := 0; i < width; i++ {
		v = v<<8 | uint64(b[*pos+i])
	}
	*pos += width
	return v, true
}

func putUint(v uint64, width int) []byte {
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		out[i] = byte(v)
		v >>= 8
	}
	return out
}

func be16(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }
