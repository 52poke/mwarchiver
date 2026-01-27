package archiver

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/mudkipme/mwarchiver/internal/mediawiki"
	_ "modernc.org/sqlite"
)

type Archiver struct {
	DBPath string
	DB     *sql.DB
	Client *mediawiki.Client
}

// NewArchiver creates a new archiver.
func NewArchiver(dbPath, apiURL, userAgent string) (*Archiver, error) {
	db, err := openDatabase(dbPath)
	if err != nil {
		return nil, err
	}

	return &Archiver{
		DBPath: dbPath,
		DB:     db,
		Client: mediawiki.NewClient(apiURL, userAgent),
	}, nil
}

func openDatabase(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		_ = db.Close()
		return nil, err
	}

	schema := `
CREATE TABLE IF NOT EXISTS pages (
	page_id INTEGER PRIMARY KEY,
	namespace INTEGER NOT NULL,
	title TEXT NOT NULL,
	text TEXT NOT NULL,
	rev_id INTEGER,
	parent_id INTEGER,
	rev_timestamp TEXT,
	rev_sha1 TEXT,
	rev_size INTEGER,
	content_model TEXT,
	content_format TEXT,
	retrieved_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pages_namespace_title ON pages(namespace, title);
`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func (a *Archiver) Close() error {
	if a.DB == nil {
		return nil
	}
	return a.DB.Close()
}

func (a *Archiver) ArchiveNamespace(namespace int, limit int) error {
	articles, err := a.Client.GetArticles(namespace, limit)
	if err != nil {
		return err
	}

	if limit > 0 && len(articles) > limit {
		articles = articles[:limit]
	}

	for _, article := range articles {
		if err := a.Archive(namespace, article.PageID, article.Title); err != nil {
			slog.Error("Failed to archive article", slog.Int("pageID", article.PageID), slog.String("title", article.Title), slog.String("error", err.Error()))
		}
	}

	return nil
}

func (a *Archiver) Archive(namespace int, pageId int, title string) error {
	page, err := a.Client.GetPage(title)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	query := `
INSERT INTO pages (
	page_id, namespace, title, text, rev_id, parent_id,
	rev_timestamp, rev_sha1, rev_size, content_model, content_format, retrieved_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(page_id) DO UPDATE SET
	namespace=excluded.namespace,
	title=excluded.title,
	text=excluded.text,
	rev_id=excluded.rev_id,
	parent_id=excluded.parent_id,
	rev_timestamp=excluded.rev_timestamp,
	rev_sha1=excluded.rev_sha1,
	rev_size=excluded.rev_size,
	content_model=excluded.content_model,
	content_format=excluded.content_format,
	retrieved_at=excluded.retrieved_at;
`
	_, err = a.DB.Exec(
		query,
		page.PageID,
		namespace,
		page.Title,
		page.Text,
		page.RevID,
		page.ParentID,
		page.Timestamp,
		page.Sha1,
		page.Size,
		page.ContentModel,
		page.ContentFormat,
		now,
	)
	return err
}
