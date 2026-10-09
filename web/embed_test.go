package web

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// The stylesheet is named after its own contents, so the page is where its current
// name is written down.
var cssAsset = regexp.MustCompile(`/assets/[^"']+\.css`)

func TestDistServesTheBuiltFrontend(t *testing.T) {
	dist, err := Dist()
	if err != nil {
		t.Fatal(err)
	}
	f, err := dist.Open("index.html")
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Fatal("embedded index.html is empty")
	}

	// The rest of the build has to be embedded too; the server hands these out
	// straight from this FS, so a missing assets dir would only show up in a browser.
	entries, err := fs.ReadDir(dist, "assets")
	if err != nil {
		t.Fatalf("assets: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no embedded assets")
	}
	name := "assets/" + entries[0].Name()
	f2, err := dist.Open(name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer f2.Close()
	if _, err := io.ReadAll(f2); err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
}

// TestPwaShellShipsWithItsIcons is the check a browser would otherwise only make
// at install time, when the failure is a home-screen icon that never appears.
func TestPwaShellShipsWithItsIcons(t *testing.T) {
	dist, err := Dist()
	if err != nil {
		t.Fatal(err)
	}

	page, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(page, []byte(`/manifest.json`)) {
		t.Fatal("index.html never links the manifest, so no browser will install")
	}

	raw, err := fs.ReadFile(dist, "manifest.json")
	if err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	var m struct {
		Name        string `json:"name"`
		ID          string `json:"id"`
		Lang        string `json:"lang"`
		StartURL    string `json:"start_url"`
		Display     string `json:"display"`
		Orientation string `json:"orientation"`
		Icons       []struct {
			Src string `json:"src"`
		}
		Screenshots []struct {
			Src        string `json:"src"`
			FormFactor string `json:"form_factor"`
		}
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest.json is not JSON: %v", err)
	}
	if m.Name == "" || m.StartURL != "/" || m.Display != "standalone" {
		t.Fatalf("manifest looks wrong: %+v", m)
	}
	// id pins which installed app a browser is looking at: without it the identity
	// is derived from start_url, so changing that address would strand every copy
	// already on a home screen. Equal to start_url keeps the derived id the same.
	if m.ID != m.StartURL {
		t.Fatalf("id = %q, want it pinned to start_url %q", m.ID, m.StartURL)
	}
	if m.Lang == "" {
		t.Fatal("no lang, so a screen reader reads the Chinese with an English voice")
	}
	// A horizontal stitched screenshot is as ordinary as a vertical one, which is
	// the whole reason this is not locked to one side.
	if m.Orientation != "any" {
		t.Fatalf("orientation = %q, want any", m.Orientation)
	}
	if len(m.Icons) == 0 {
		t.Fatal("no icons in the manifest")
	}
	for _, icon := range m.Icons {
		if _, err := fs.Stat(dist, strings.TrimPrefix(icon.Src, "/")); err != nil {
			t.Errorf("manifest points at %s: %v", icon.Src, err)
		}
	}
	// A screenshot that 404s is worse than none: the install dialog shows a broken
	// preview for it, and only on the devices that read that field.
	for _, shot := range m.Screenshots {
		if shot.FormFactor != "narrow" {
			t.Errorf("screenshot %s: form_factor = %q, want narrow", shot.Src, shot.FormFactor)
		}
		if _, err := fs.Stat(dist, strings.TrimPrefix(shot.Src, "/")); err != nil {
			t.Errorf("manifest points at %s: %v", shot.Src, err)
		}
	}

	if _, err := fs.ReadFile(dist, "sw.js"); err != nil {
		t.Fatalf("sw.js: %v", err)
	}
}

// TestThemeTravelsWithTheShell pins the two halves of the theme mechanism that a
// browser only reveals: which theme shows on the very first paint, and whether
// switching it later changes anything at all.
func TestThemeTravelsWithTheShell(t *testing.T) {
	dist, err := Dist()
	if err != nil {
		t.Fatal(err)
	}
	page, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)

	// The bundle that owns the theme is deferred until after the first paint, so
	// the choice has to be read and applied in the page itself — otherwise a
	// light-theme visitor is shown a dark page and then watches it flip.
	booted := strings.Index(html, "tuqie-theme")
	bundle := strings.Index(html, `type="module"`)
	if booted < 0 {
		t.Fatal("index.html decides no theme of its own, so the theme arrives a repaint late")
	}
	if bundle < 0 {
		t.Fatal("index.html loads no bundle")
	}
	if booted > bundle {
		t.Fatalf("the theme bootstrap sits after the bundle (%d > %d)", booted, bundle)
	}

	name := cssAsset.FindString(html)
	if name == "" {
		t.Fatal("index.html names no stylesheet")
	}
	css, err := fs.ReadFile(dist, strings.TrimPrefix(name, "/"))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	sheet := string(css)
	// A utility that baked its colour in stays that colour whatever the theme says.
	if !strings.Contains(sheet, "var(--p-surface)") {
		t.Fatal("no utility reads the palette at runtime, so switching the theme changes nothing")
	}
	// The second palette has to hang off the very attribute the page sets.
	if !strings.Contains(sheet, "[data-theme=light]") && !strings.Contains(sheet, `[data-theme="light"]`) {
		t.Fatal("the built CSS carries no light override keyed on data-theme")
	}
}
