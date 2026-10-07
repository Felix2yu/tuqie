package split

import (
	"bytes"
	"encoding/binary"
	"testing"

	"tuqie/internal/exif"

	"github.com/gen2brain/avif"
)

// avifSkeleton is the smallest file the rewriter will accept: a meta box naming
// item 1 as primary, an iloc placing it in mdat, and an iinf describing it.
func avifSkeleton(t *testing.T, mutate func(meta [][]byte) [][]byte) []byte {
	t.Helper()
	image := bytes.Repeat([]byte{0xAA}, 64)
	metaBody := func() [][]byte {
		iloc := []byte{0, 0, 0, 0, 0x44, 0x00}
		iloc = append(iloc, 0, 1)       // one item
		iloc = append(iloc, 0, 1)       // item_ID 1
		iloc = append(iloc, 0, 0)       // data_reference_index
		iloc = append(iloc, 0, 1)       // one extent
		iloc = append(iloc, 0, 0, 0, 0) // extent offset, filled below
		iloc = append(iloc, byte(len(image)>>24), byte(len(image)>>16), byte(len(image)>>8), byte(len(image)))
		infe := isoBox("infe", []byte{2, 0, 0, 0, 0, 1, 0, 0}, []byte("av01"), []byte("Color\x00"))
		iinf := append([]byte{0, 0, 0, 0, 0, 1}, infe...)
		return [][]byte{
			isoBox("pitm", []byte{0, 0, 0, 0}, []byte{0, 1}),
			isoBox("iloc", iloc),
			isoBox("iinf", iinf),
		}
	}
	kids := metaBody()
	if mutate != nil {
		kids = mutate(kids)
	}
	meta := append([]byte{0, 0, 0, 0}, bytes.Join(kids, nil)...)
	// The image sits at the end of mdat's start; recompute the extent offset.
	head := len(isoBox("ftyp", []byte("avif"))) + len(meta) + 8
	for i, k := range kids {
		if len(k) > 8 && string(k[4:8]) == "iloc" {
			binary.BigEndian.PutUint32(k[len(k)-8:], uint32(head))
			kids[i] = k
			meta = append([]byte{0, 0, 0, 0}, bytes.Join(kids, nil)...)
		}
	}
	file := isoBox("ftyp", []byte("avif"))
	file = append(file, isoBox("meta", meta)...)
	return append(file, isoBox("mdat", image)...)
}

func TestAvifWithDateRefusesWhatItCannotTrust(t *testing.T) {
	tiff := append([]byte("II*\x00"), make([]byte, 16)...)

	for name, file := range map[string][]byte{
		"no meta":    isoBox("ftyp", []byte("avif")),
		"mdat first": append(isoBox("mdat", []byte("pixels")), isoBox("meta", []byte{0, 0, 0, 0})...),
		"no primary": avifSkeleton(t, func(k [][]byte) [][]byte {
			return [][]byte{k[1], k[2]}
		}),
		"iloc with two extents": avifSkeleton(t, func(k [][]byte) [][]byte {
			iloc := k[1]
			binary.BigEndian.PutUint16(iloc[len(iloc)-14:], 2) // extent_count
			out := append([]byte{}, iloc[:len(iloc)-8]...)
			out = append(out, 0, 0, 0, 0, 0, 0, 0, 4)
			return [][]byte{k[0], append(out, iloc[len(iloc)-8:]...), k[2]}
		}),
		"already referenced": avifSkeleton(t, func(k [][]byte) [][]byte {
			return append(k, isoBox("iref", []byte{0, 0, 0, 0},
				isoBox("cdsc", []byte{0, 1}, []byte{0, 0, 0, 1})))
		}),
		"picture stored in idat": avifSkeleton(t, func(k [][]byte) [][]byte {
			// iloc version 1 with construction_method 1: the bytes live inside
			// meta, where a second rewrite of the offsets would be needed.
			head := []byte{1, 0, 0, 0, 0x44, 0x40, 0, 1, 0, 1, 0, 1, 0, 0}
			entry := append(head, k[1][8:12]...)
			entry = append(entry, k[1][12:]...)
			return [][]byte{k[0], isoBox("iloc", entry), k[2]}
		}),
		"truncated iloc": avifSkeleton(t, func(k [][]byte) [][]byte {
			return [][]byte{k[0], k[1][:12], k[2]}
		}),
	} {
		t.Run(name, func(t *testing.T) {
			if got := avifWithDate(file, tiff); !bytes.Equal(got, file) {
				t.Errorf("rewrote a file it should have left alone (%d → %d bytes)", len(file), len(got))
			}
		})
	}
}

func TestAvifWithDateAddsTheItem(t *testing.T) {
	taken := exif.Date{Wall: "2026:10:07 09:15:30"}
	tiff := taken.TIFF()
	file := avifWithDate(avifSkeleton(t, nil), tiff)
	if bytes.Equal(file, avifSkeleton(t, nil)) {
		t.Fatal("nothing was added")
	}
	e, err := avif.DecodeExif(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("the rewritten file no longer reports its Exif: %v", err)
	}
	if e.DateTimeOriginal != taken.Wall {
		t.Errorf("date read back = %q, want %q", e.DateTimeOriginal, taken.Wall)
	}
}

func TestAvifWithDateFollowsHowTheFileChoosesToCount(t *testing.T) {
	// iloc may carry the position in the base offset instead of the extent offset,
	// and iinf may count its items in 32 bits. Both are shapes a re-encode could
	// meet, and getting either wrong moves the picture rather than the date.
	image := bytes.Repeat([]byte{0xAA}, 64)
	taken := exif.Date{Wall: "2026:10:07 09:15:30"}
	tiff := taken.TIFF()

	iloc := []byte{0, 0, 0, 0, 0x44, 0x40} // offset 4, length 4, base 4
	iloc = append(iloc, 0, 1)
	iloc = append(iloc, 0, 1)       // item 1
	iloc = append(iloc, 0, 0)       // data_reference_index
	iloc = append(iloc, 0, 0, 0, 0) // base offset, patched below
	iloc = append(iloc, 0, 1)       // one extent
	iloc = append(iloc, 0, 0, 0, 0) // extent offset
	iloc = append(iloc, byte(len(image)>>24), byte(len(image)>>16), byte(len(image)>>8), byte(len(image)))
	infe := isoBox("infe", []byte{2, 0, 0, 0, 0, 1, 0, 0}, []byte("av01"), []byte("Color\x00"))
	ftyp := isoBox("ftyp", []byte("avif"))
	pitm := isoBox("pitm", []byte{0, 0, 0, 0}, []byte{0, 1})
	// Version 1 counts its items in 32 bits.
	iinf := isoBox("iinf", []byte{1, 0, 0, 0}, []byte{0, 0, 0, 1}, infe)
	// The base offset is the fourth field of the entry: six bytes of header, two
	// for the count, two for the item id and two for the data reference index.
	const baseAt = 12
	place := func(base uint32) []byte {
		b := append([]byte{}, iloc...)
		binary.BigEndian.PutUint32(b[baseAt:], base)
		return bytes.Join([][]byte{{0, 0, 0, 0}, pitm, isoBox("iloc", b), iinf}, nil)
	}
	head := uint32(len(ftyp) + len(isoBox("meta", place(0))) + 8)
	file := append(append(ftyp, isoBox("meta", place(head))...), isoBox("mdat", image)...)

	got := avifWithDate(file, tiff)
	if bytes.Equal(got, file) {
		t.Fatal("the file came back untouched")
	}
	e, err := avif.DecodeExif(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("Exif not readable: %v", err)
	}
	if e.DateTimeOriginal != taken.Wall {
		t.Errorf("date = %q, want %q", e.DateTimeOriginal, taken.Wall)
	}
	// The picture is filler rather than AV1 here, so what this pins is that the
	// entry still points at it: same base-offset placement, same 64 bytes, with
	// the Exif payload following it inside mdat.
	var items []ilocItem
	for _, b := range splitBoxes(got) {
		if b.name != "meta" {
			continue
		}
		for _, c := range splitBoxes(b.body[4:]) {
			if c.name == "iloc" {
				var ok bool
				if items, _, ok = readIloc(c.body); !ok {
					t.Fatal("iloc no longer parses")
				}
			}
		}
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want the picture and its Exif", len(items))
	}
	// meta grew, so the picture moved with it; what has to hold is that both
	// extents point at where their bytes actually are in the rewritten file.
	var mdatAt, walked uint64
	for _, b := range splitBoxes(got) {
		if b.name == "mdat" {
			mdatAt = walked + 8
		}
		walked += uint64(len(b.full))
	}
	if items[0].pos != mdatAt || items[0].length != uint64(len(image)) {
		t.Errorf("picture extent %d + %d, want %d + %d", items[0].pos, items[0].length, mdatAt, len(image))
	}
	if want := mdatAt + uint64(len(image)); items[1].pos != want || items[1].length != uint64(len(tiff)+4) {
		t.Errorf("exif extent %d + %d, want %d + %d", items[1].pos, items[1].length, want, len(tiff)+4)
	}
}
