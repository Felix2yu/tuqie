package main

import (
	"testing"
)

func TestEnvOrFallsBackToTheBuiltInDefault(t *testing.T) {
	t.Setenv("TUQIE_MAX_UPLOAD", "")
	if got := envOr("max-upload", defaultMaxUpload); got != defaultMaxUpload {
		t.Errorf("empty variable: got %q", got)
	}
	t.Setenv("TUQIE_MAX_UPLOAD", "2GB")
	if got := envOr("max-upload", defaultMaxUpload); got != "2GB" {
		t.Errorf("variable: got %q", got)
	}
	// The flag name's own dashes are the underscore the variable spells.
	t.Setenv("TUQIE_UPLOADS_PER_MINUTE", "5")
	if got := envOr("uploads-per-minute", "30"); got != "5" {
		t.Errorf("dashed flag: got %q", got)
	}
}

// The default is what the page and the README quote, so it has to be a size the
// parser reads and the number those two say. It also has to clear the 32MB PNG
// long screenshot the README measures, which is the reason it is not smaller.
func TestDefaultCeiling(t *testing.T) {
	got, err := parseSize(defaultMaxUpload)
	if err != nil {
		t.Fatal(err)
	}
	if got != 35<<20 {
		t.Fatalf("%s = %d bytes, want %d", defaultMaxUpload, got, 35<<20)
	}
	if got <= 32<<20 {
		t.Fatalf("%s = %d bytes, which would refuse the 32MB sample", defaultMaxUpload, got)
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"15728640", 15 << 20},
		{"15MB", 15 << 20},
		{"15mb", 15 << 20},
		{"15 M", 15 << 20},
		{"15M", 15 << 20},
		{"1.5GB", 1<<30 + 1<<29},
		{"512K", 512 << 10},
		{"1KB", 1 << 10},
		{"200", 200},
		{"0", 0},
	}
	for _, c := range cases {
		got, err := parseSize(c.in)
		if err != nil {
			t.Errorf("parseSize(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseSizeRejectsNonsense(t *testing.T) {
	for _, in := range []string{"", "   ", "abc", "-5MB", "MB", "10000000TB", "1e30"} {
		if got, err := parseSize(in); err == nil {
			t.Errorf("parseSize(%q) = %d, want an error", in, got)
		}
	}
}
