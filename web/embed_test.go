package web

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"strings"
	"testing"
)

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
		Name     string `json:"name"`
		StartURL string `json:"start_url"`
		Display  string `json:"display"`
		Icons    []struct {
			Src string `json:"src"`
		}
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest.json is not JSON: %v", err)
	}
	if m.Name == "" || m.StartURL != "/" || m.Display != "standalone" {
		t.Fatalf("manifest looks wrong: %+v", m)
	}
	if len(m.Icons) == 0 {
		t.Fatal("no icons in the manifest")
	}
	for _, icon := range m.Icons {
		if _, err := fs.Stat(dist, strings.TrimPrefix(icon.Src, "/")); err != nil {
			t.Errorf("manifest points at %s: %v", icon.Src, err)
		}
	}

	if _, err := fs.ReadFile(dist, "sw.js"); err != nil {
		t.Fatalf("sw.js: %v", err)
	}
}
