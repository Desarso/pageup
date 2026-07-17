package sitebundle

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackAndParseNestedHTMLSiteDeterministically(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "index.html", `<a href="about.html">About</a>`)
	writeTestFile(t, root, "about.html", `<h1>About</h1>`)
	writeTestFile(t, root, "docs/index.html", `<h1>Docs</h1>`)

	first, err := Pack(root, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Pack(root, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("packing the same site produced different archives")
	}

	site, err := Parse(first, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(site.Files) != 3 || string(site.Files["docs/index.html"]) != `<h1>Docs</h1>` {
		t.Fatalf("unexpected parsed site: %#v", site.Files)
	}
}

func TestPackRequiresRootIndexAndHTMLOnly(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "about.html", "about")
	if _, err := Pack(root, DefaultMaxBytes); err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("missing index error = %v", err)
	}

	writeTestFile(t, root, "index.html", "index")
	writeTestFile(t, root, "style.css", "body {}")
	if _, err := Pack(root, DefaultMaxBytes); err == nil || !strings.Contains(err.Error(), "only .html") {
		t.Fatalf("non-HTML error = %v", err)
	}
}

func TestPackEnforcesFileAndByteLimits(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "index.html", "index")
	for index := 0; index < MaxFiles; index++ {
		writeTestFile(t, root, filepath.Join("pages", fmt.Sprintf("%03d.html", index)), "")
	}
	if _, err := Pack(root, DefaultMaxBytes); err == nil || !strings.Contains(err.Error(), "100 HTML file") {
		t.Fatalf("file limit error = %v", err)
	}

	small := t.TempDir()
	writeTestFile(t, small, "index.html", "12345")
	if _, err := Pack(small, 4); err == nil || !strings.Contains(err.Error(), "upload limit") {
		t.Fatalf("byte limit error = %v", err)
	}
}

func TestParseRejectsUnsafeArchivePaths(t *testing.T) {
	archive := makeTestArchive(t, map[string]string{
		"index.html":   "index",
		"../evil.html": "evil",
	})
	if _, err := Parse(archive, DefaultMaxBytes); err == nil || !strings.Contains(err.Error(), "invalid path") {
		t.Fatalf("unsafe path error = %v", err)
	}
}

func writeTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func makeTestArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, contents := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
