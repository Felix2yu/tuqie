// Package exif reads and writes the capture date stored in image metadata.
//
// Only dates are handled: a re-encoded slice needs the time the original was
// taken, not the camera's whole metadata block.
package exif

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Date is a capture timestamp as the camera wrote it. EXIF keeps DateTimeOriginal
// without a zone, so the wall-clock text is carried verbatim rather than re-parsed
// in whatever zone the server happens to run in.
type Date struct {
	Wall   string // "2026:10:07 14:23:45"
	Subsec string // fractional digits, may be empty
	Offset string // "+08:00", may be empty
}

const wallLayout = "2006:01:02 15:04:05"

// TIFF tags this package cares about.
const (
	tagDateTime       = 0x0132
	tagExifIFD        = 0x8769
	tagDateOriginal   = 0x9003
	tagDateDigitized  = 0x9004
	tagOffsetOriginal = 0x9011
	tagOffsetDigi     = 0x9012
	tagSubSec         = 0x9290
	tagSubSecOrig     = 0x9291
	tagSubSecDigi     = 0x9292

	typeByte  = 1
	typeASCII = 2
	typeShort = 3
	typeLong  = 4
)

// Valid reports whether d carries a date that can be used.
func (d Date) Valid() bool {
	_, err := d.Time()
	return err == nil
}

// Time converts d to an instant. Without an offset in the metadata the wall clock
// is read in the server's local zone, which is the best available guess.
func (d Date) Time() (time.Time, error) {
	wall := strings.TrimSpace(d.Wall)
	if len(wall) < len(wallLayout) {
		return time.Time{}, fmt.Errorf("exif: short date %q", d.Wall)
	}
	value, layout := wall[:len(wallLayout)], wallLayout
	if sub := digitsOnly(d.Subsec); sub != "" {
		if len(sub) > 6 {
			sub = sub[:6]
		}
		value += "." + sub
		layout += ".999999"
	}
	loc := time.Local
	if secs, ok := parseOffset(d.Offset); ok {
		loc = time.FixedZone("", secs)
	}
	t, err := time.ParseInLocation(layout, value, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("exif: %q: %w", d.Wall, err)
	}
	return t, nil
}

// FromTime builds a Date for an instant that arrived without metadata text, such as
// the modification time a browser reports for an uploaded file. Wall and offset
// together describe the instant exactly, whatever zone this process runs in.
func FromTime(t time.Time) Date {
	_, secs := t.Zone()
	sign := "+"
	if secs < 0 {
		sign, secs = "-", -secs
	}
	return Date{
		Wall:   t.Format(wallLayout),
		Subsec: fmt.Sprintf("%03d", t.Nanosecond()/int(time.Millisecond)),
		Offset: fmt.Sprintf("%s%02d:%02d", sign, secs/3600, secs%3600/60),
	}
}

// Zip timestamps carry a DOS date, which can only express 1980 to 2107. Dates
// outside that window are dropped rather than written as a half-supported value, so
// the same instant lands in both the image metadata and the archive entry.
const (
	yearMin = 1980
	yearMax = 2107
)

// InZipRange reports whether d is a usable date that zip can also stamp.
func (d Date) InZipRange() bool {
	t, err := d.Time()
	if err != nil {
		return false
	}
	y := t.Year()
	return y >= yearMin && y <= yearMax
}

// Scan finds the capture date in the leading bytes of an encoded image. It returns
// the zero Date for formats and files that carry none.
func Scan(data []byte) Date {
	switch {
	case hasPrefix(data, []byte{0xff, 0xd8}):
		return scanJPEG(data)
	case hasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return scanPNG(data)
	}
	return Date{}
}

func scanJPEG(data []byte) Date {
	// Walk the marker segments that sit between SOI and the first scan, and hand
	// the payload of any Exif APP1 to the TIFF reader.
	for i := 2; i+1 < len(data); {
		if data[i] != 0xff {
			return Date{}
		}
		marker := data[i+1]
		if marker == 0xff { // fill byte before a marker
			i++
			continue
		}
		if marker == 0xda || marker == 0xd9 { // scan data: no metadata follows
			return Date{}
		}
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) { // no payload
			i += 2
			continue
		}
		if i+4 > len(data) {
			return Date{}
		}
		segLen := int(data[i+2])<<8 | int(data[i+3])
		if segLen < 2 || i+2+segLen > len(data) {
			return Date{}
		}
		payload := data[i+4 : i+2+segLen]
		if marker == 0xe1 && hasPrefix(payload, []byte("Exif\x00\x00")) {
			if d := readTIFF(payload[6:]); d.Valid() {
				return d
			}
		}
		i += 2 + segLen
	}
	return Date{}
}

func scanPNG(data []byte) Date {
	for i := 8; i+12 <= len(data); {
		chunkLen := int(binary.BigEndian.Uint32(data[i : i+4]))
		if chunkLen < 0 || i+12+chunkLen > len(data) {
			return Date{}
		}
		kind := string(data[i+4 : i+8])
		body := data[i+8 : i+8+chunkLen]
		if kind == "IDAT" { // eXIf has to precede the image data to be trusted
			return Date{}
		}
		if kind == "eXIf" {
			if d := readTIFF(body); d.Valid() {
				return d
			}
		}
		i += 12 + chunkLen
	}
	return Date{}
}

type entry struct {
	tag, typ uint16
	count    uint32
	// value holds the field bytes: inline when they fit in 4 bytes, elsewhere read
	// from valOff.
	value  []byte
	valOff int
}

// readIFD returns the date-relevant entries of one IFD.
func readIFD(buf []byte, off int, order binary.ByteOrder) (map[uint16]entry, bool) {
	if off < 0 || off+2 > len(buf) {
		return nil, false
	}
	n := int(order.Uint16(buf[off : off+2]))
	if n <= 0 || n > 1024 || off+2+n*12+4 > len(buf) {
		return nil, false
	}
	out := make(map[uint16]entry, n)
	for k := 0; k < n; k++ {
		p := off + 2 + k*12
		e := entry{
			tag:    order.Uint16(buf[p : p+2]),
			typ:    order.Uint16(buf[p+2 : p+4]),
			count:  order.Uint32(buf[p+4 : p+8]),
			valOff: int(order.Uint32(buf[p+8 : p+12])),
		}
		size := int(e.count) * typeLen(e.typ)
		switch {
		case e.typ != typeASCII && e.typ != typeByte && e.typ != typeShort && e.typ != typeLong:
			continue
		case size <= 0 || size > 1<<16:
			continue
		case size <= 4:
			e.value = buf[p+8 : p+12][:size]
		case e.valOff >= 0 && e.valOff+size <= len(buf):
			e.value = buf[e.valOff : e.valOff+size]
		default:
			continue
		}
		out[e.tag] = e
	}
	return out, true
}

func (e entry) text() string {
	if e.typ != typeASCII {
		return ""
	}
	return strings.TrimRight(string(e.value), "\x00 ")
}

func readTIFF(buf []byte) Date {
	if len(buf) < 8 {
		return Date{}
	}
	var order binary.ByteOrder
	switch {
	case buf[0] == 'I' && buf[1] == 'I':
		order = binary.LittleEndian
	case buf[0] == 'M' && buf[1] == 'M':
		order = binary.BigEndian
	default:
		return Date{}
	}
	ifd0, ok := readIFD(buf, int(order.Uint32(buf[4:8])), order)
	if !ok {
		return Date{}
	}
	var sub map[uint16]entry
	if ptr, ok := ifd0[tagExifIFD]; ok && ptr.typ == typeLong && len(ptr.value) == 4 {
		sub, _ = readIFD(buf, int(order.Uint32(ptr.value)), order)
	}

	// Camera software disagrees about which tags it fills in; try the ones readers
	// look at first.
	for _, pick := range []struct {
		subTag, secTag, offTag uint16
		subMap                 map[uint16]entry
	}{
		{tagDateOriginal, tagSubSecOrig, tagOffsetOriginal, sub},
		{tagDateDigitized, tagSubSecDigi, tagOffsetDigi, sub},
		{tagDateTime, tagSubSec, 0, ifd0},
	} {
		if pick.subMap == nil {
			continue
		}
		e, ok := pick.subMap[pick.subTag]
		if !ok {
			continue
		}
		d := Date{Wall: e.text()}
		if pick.secTag != 0 {
			if s, ok := pick.subMap[pick.secTag]; ok {
				d.Subsec = s.text()
			}
		}
		if pick.offTag != 0 {
			if o, ok := pick.subMap[pick.offTag]; ok {
				d.Offset = normalizeOffset(o.text())
			}
		}
		if d.Valid() {
			return d
		}
	}
	return Date{}
}

func typeLen(typ uint16) int {
	switch typ {
	case typeByte, typeASCII:
		return 1
	case typeShort:
		return 2
	case typeLong:
		return 4
	}
	return 0
}

// App1 builds an EXIF APP1 segment holding d's date fields, ready to splice into a
// JPEG. It returns nil when d has no usable date.
func (d Date) App1() []byte {
	if !d.Valid() {
		return nil
	}
	wall := d.wall()
	type field struct {
		tag  uint16
		text string
	}
	ifd0 := []field{{tagDateTime, wall}}
	exif := []field{{tagDateOriginal, wall}, {tagDateDigitized, wall}}
	if sub := digitsOnly(d.Subsec); sub != "" {
		exif = append(exif, field{tagSubSecOrig, sub}, field{tagSubSecDigi, sub})
	}
	if off := normalizeOffset(d.Offset); off != "" {
		exif = append(exif, field{tagOffsetOriginal, off}, field{tagOffsetDigi, off})
	}
	// Readers expect tags in ascending order within an IFD.
	sortFields := func(fs []field) {
		sort.Slice(fs, func(i, j int) bool { return fs[i].tag < fs[j].tag })
	}
	sortFields(ifd0)
	sortFields(exif)

	// IFD0 also has to point at the sub-IFD, which costs one more entry.
	exifOff := 8 + 2 + (len(ifd0)+1)*12 + 4
	dataOff := exifOff + 2 + len(exif)*12 + 4

	// Values of four bytes or fewer go inline in the entry, the way camera firmware
	// writes them; some readers assume that.
	all := append(append([]field{}, ifd0...), exif...)
	offs := make([]int, len(all))
	sizes := make([]int, len(all))
	for i, f := range all {
		sizes[i] = len(f.text) + 1
		offs[i] = -1
		if sizes[i] > 4 {
			offs[i] = dataOff
			dataOff += sizes[i]
		}
	}
	idx := 0

	var buf []byte
	put := func(b ...byte) { buf = append(buf, b...) }
	putU16 := func(v uint16) { put(byte(v), byte(v>>8)) }
	putU32 := func(v int) { put(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }
	writeASCII := func(tag uint16) {
		defer func() { idx++ }()
		putU16(tag)
		putU16(typeASCII)
		putU32(sizes[idx])
		if offs[idx] < 0 {
			put(append([]byte(all[idx].text), make([]byte, 4-sizes[idx]+1)...)...)
			return
		}
		putU32(offs[idx])
	}

	put('I', 'I', 0x2a, 0x00)
	putU32(8)

	putU16(uint16(len(ifd0) + 1))
	for _, f := range ifd0 {
		writeASCII(f.tag)
	}
	putU16(tagExifIFD)
	putU16(typeLong)
	putU32(1)
	putU32(exifOff)
	putU32(0) // no follow-on IFD: no thumbnail is carried over

	putU16(uint16(len(exif)))
	for _, f := range exif {
		writeASCII(f.tag)
	}
	putU32(0)

	for i, f := range all {
		if offs[i] >= 0 {
			put(append([]byte(f.text), 0)...)
		}
	}

	payload := append([]byte("Exif\x00\x00"), buf...)
	segLen := len(payload) + 2
	if segLen > 0xffff {
		return nil
	}
	return append([]byte{0xff, 0xe1, byte(segLen >> 8), byte(segLen)}, payload...)
}

// wall renders the stored date, padding a truncated string to the EXIF width.
func (d Date) wall() string {
	wall := strings.TrimSpace(d.Wall)
	if len(wall) > len(wallLayout) {
		wall = wall[:len(wallLayout)]
	}
	return wall
}

// InjectJPEG places an EXIF APP1 segment directly after the start-of-image marker,
// where the Exif specification puts it. Data that is not a JPEG is returned as is.
func InjectJPEG(data []byte, app1 []byte) []byte {
	if len(app1) == 0 || !hasPrefix(data, []byte{0xff, 0xd8}) {
		return data
	}
	out := make([]byte, 0, len(data)+len(app1))
	out = append(out, data[:2]...)
	out = append(out, app1...)
	return append(out, data[2:]...)
}

func normalizeOffset(s string) string {
	s = strings.TrimSpace(s)
	if _, ok := parseOffset(s); !ok {
		return ""
	}
	return s
}

func parseOffset(s string) (int, bool) {
	if len(s) != 6 || (s[0] != '+' && s[0] != '-') || s[3] != ':' {
		return 0, false
	}
	h, m := digitsOnly(s[1:3]), digitsOnly(s[4:6])
	if h == "" || m == "" {
		return 0, false
	}
	hh, mm := atoi(h), atoi(m)
	if hh > 23 || mm > 59 {
		return 0, false
	}
	secs := hh*3600 + mm*60
	if s[0] == '-' {
		secs = -secs
	}
	return secs, true
}

func digitsOnly(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func hasPrefix(b []byte, prefix []byte) bool {
	return len(b) >= len(prefix) && string(b[:len(prefix)]) == string(prefix)
}
