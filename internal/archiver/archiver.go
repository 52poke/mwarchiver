package archiver

import (
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mudkipme/mwarchiver/internal/mediawiki"
)

var (
	sanitizeRegex = regexp.MustCompile(`[^\p{L}\p{N}_.-]`)
)

type Archiver struct {
	OutputPath string
	Client     *mediawiki.Client
}

// NewArchiver creates a new archiver.
func NewArchiver(outputPath, apiURL string) *Archiver {
	return &Archiver{
		OutputPath: outputPath,
		Client:     mediawiki.NewClient(apiURL),
	}
}

func (a *Archiver) ArchiveNamespace(namespace int, limit int) error {
	articles, err := a.Client.GetArticles(namespace, limit)
	if err != nil {
		return err
	}

	for _, article := range articles {
		if err := a.Archive(namespace, article.PageID, article.Title); err != nil {
			slog.Error("Failed to archive article", slog.Int("pageID", article.PageID), slog.String("title", article.Title), slog.String("error", err.Error()))
		}
	}

	return nil
}

func (a *Archiver) Archive(namespace int, pageId int, title string) error {
	dir := path.Join(a.OutputPath, fmt.Sprintf("namespace_%d", namespace))
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	fileName := filepath.Join(dir, fmt.Sprintf("%d_%s.txt", pageId, sanitizeTitle(title)))
	if _, err := os.Stat(fileName); err == nil {
		return nil
	}

	content, err := a.Client.GetPage(title)
	if err != nil {
		return err
	}
	content = "Title: " + title + "\n\n" + content
	return os.WriteFile(fileName, []byte(content), 0644)
}

func sanitizeTitle(title string) string {
	sanitized := strings.ReplaceAll(title, " ", "_")
	sanitized = sanitizeRegex.ReplaceAllString(sanitized, "")
	sanitized = strings.Trim(sanitized, "_.")
	return sanitized
}
