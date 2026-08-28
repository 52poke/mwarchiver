package restorer

import (
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCountDumpPages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	xml := []byte("<mediawiki><page><title>One</title></page>\n<page><text>&lt;page&gt;</text></page></mediawiki>")
	plainPath := filepath.Join(root, "dump.xml")
	if err := os.WriteFile(plainPath, xml, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := CountDumpPages(context.Background(), plainPath); err != nil || got != 2 {
		t.Fatalf("plain page count = %d, %v; want 2", got, err)
	}

	gzipPath := filepath.Join(root, "dump.xml.gz")
	file, err := os.Create(gzipPath)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	if _, err := compressed.Write(xml); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := CountDumpPages(context.Background(), gzipPath); err != nil || got != 2 {
		t.Fatalf("gzip page count = %d, %v; want 2", got, err)
	}
}

func TestElementCounterFindsElementAcrossWrites(t *testing.T) {
	t.Parallel()

	counter := &elementCounter{ctx: context.Background(), element: pageElement}
	for _, part := range []string{"before<pa", "ge>middle<page", ">after"} {
		if _, err := counter.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if counter.count != 2 {
		t.Fatalf("page count = %d, want 2", counter.count)
	}
}
