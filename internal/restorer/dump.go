package restorer

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var pageElement = []byte("<page>")

// CountDumpPages counts MediaWiki's unescaped <page> elements without
// materializing the dump. Wikitext occurrences are XML-escaped, so they do not
// produce false positives.
func CountDumpPages(ctx context.Context, path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open dump: %w", err)
	}
	defer file.Close()

	var reader io.Reader = file
	var closeReader func() error
	var command *exec.Cmd

	switch strings.ToLower(filepath.Ext(path)) {
	case ".gz":
		compressed, err := gzip.NewReader(file)
		if err != nil {
			return 0, fmt.Errorf("open gzip dump: %w", err)
		}
		reader = compressed
		closeReader = compressed.Close
	case ".bz2":
		reader = bzip2.NewReader(file)
	case ".7z":
		if _, err := exec.LookPath("7za"); err != nil {
			return 0, fmt.Errorf("count .7z dump pages: find 7za: %w", err)
		}
		command = exec.CommandContext(ctx, "7za", "e", "-so", path)
		pipe, err := command.StdoutPipe()
		if err != nil {
			return 0, fmt.Errorf("read .7z dump: %w", err)
		}
		command.Stderr = io.Discard
		if err := command.Start(); err != nil {
			return 0, fmt.Errorf("open .7z dump: %w", err)
		}
		reader = pipe
	}
	if closeReader != nil {
		defer closeReader()
	}

	counter := &elementCounter{ctx: ctx, element: pageElement}
	if _, err := io.Copy(counter, reader); err != nil {
		if command != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
		return 0, fmt.Errorf("scan dump pages: %w", err)
	}
	if command != nil {
		if err := command.Wait(); err != nil {
			return 0, fmt.Errorf("decompress .7z dump: %w", err)
		}
	}
	return counter.count, nil
}

type elementCounter struct {
	ctx     context.Context
	element []byte
	tail    []byte
	count   int64
}

func (c *elementCounter) Write(data []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
	}

	combined := make([]byte, 0, len(c.tail)+len(data))
	combined = append(combined, c.tail...)
	combined = append(combined, data...)
	c.count += int64(bytes.Count(combined, c.element))

	tailLength := len(c.element) - 1
	if tailLength > len(combined) {
		tailLength = len(combined)
	}
	c.tail = append(c.tail[:0], combined[len(combined)-tailLength:]...)
	return len(data), nil
}
