package web

import (
	"io"
	"io/fs"
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
