package mediawiki

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
)

type Client struct {
	// APIURL is the URL of the MediaWiki API.
	APIURL string
	// UserAgent is the User-Agent header to send with requests.
	UserAgent string
}

// NewClient creates a new MediaWiki client.
func NewClient(apiURL, userAgent string) *Client {
	return &Client{
		APIURL:    apiURL,
		UserAgent: userAgent,
	}
}

type GetPageResponse struct {
	Query struct {
		Pages map[string]struct {
			PageID    int    `json:"pageid"`
			Namespace int    `json:"ns"`
			Title     string `json:"title"`
			Revisions []struct {
				RevID     int    `json:"revid"`
				ParentID  int    `json:"parentid"`
				Timestamp string `json:"timestamp"`
				Size      int    `json:"size"`
				Sha1      string `json:"sha1"`
				Slots     struct {
					Main struct {
						Content       string `json:"*"`
						ContentModel  string `json:"contentmodel"`
						ContentFormat string `json:"contentformat"`
					} `json:"main"`
				} `json:"slots"`
			} `json:"revisions"`
		} `json:"pages"`
	} `json:"query"`
}

type PageContent struct {
	PageID        int
	Namespace     int
	Title         string
	Text          string
	RevID         int
	ParentID      int
	Timestamp     string
	Size          int
	Sha1          string
	ContentModel  string
	ContentFormat string
}

// GetPage gets a page from the MediaWiki API.
func (c *Client) GetPage(title string) (*PageContent, error) {
	slog.Info("Getting page", slog.String("title", title))

	params := fmt.Sprintf("action=query&format=json&titles=%s&prop=revisions&rvlimit=1&rvprop=content|ids|timestamp|size|sha1&rvslots=*", url.QueryEscape(title))
	req, err := http.NewRequest(http.MethodGet, c.APIURL+"?"+params, nil)
	if err != nil {
		return nil, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result GetPageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	for _, page := range result.Query.Pages {
		if len(page.Revisions) > 0 {
			revision := page.Revisions[0]
			return &PageContent{
				PageID:        page.PageID,
				Namespace:     page.Namespace,
				Title:         page.Title,
				Text:          revision.Slots.Main.Content,
				RevID:         revision.RevID,
				ParentID:      revision.ParentID,
				Timestamp:     revision.Timestamp,
				Size:          revision.Size,
				Sha1:          revision.Sha1,
				ContentModel:  revision.Slots.Main.ContentModel,
				ContentFormat: revision.Slots.Main.ContentFormat,
			}, nil
		}
	}

	return nil, fmt.Errorf("no wikitext found for title: %s", title)
}

type Continue struct {
	Continue   string `json:"continue"`
	Apcontinue string `json:"apcontinue"`
}

type Article struct {
	PageID int    `json:"pageid"`
	Title  string `json:"title"`
	Ns     int    `json:"ns"`
}

type GetArticleResponse struct {
	Query struct {
		AllPages []*Article `json:"allpages"`
	} `json:"query"`
	Continue *Continue `json:"continue"`
}

// GetArticles gets all articles in a namespace from the MediaWiki API.
func (c *Client) GetArticles(namespace, limit int) ([]*Article, error) {
	var articles []*Article
	var continueToken *Continue

	for {
		slog.Info("Getting articles", slog.Int("namespace", namespace), slog.Any("continueToken", continueToken))

		params := fmt.Sprintf("action=query&format=json&list=allpages&apnamespace=%d&aplimit=max", namespace)
		if continueToken != nil {
			params += "&continue=" + url.QueryEscape(continueToken.Continue) + "&apcontinue=" + url.QueryEscape(continueToken.Apcontinue)
		}
		req, err := http.NewRequest(http.MethodGet, c.APIURL+"?"+params, nil)
		if err != nil {
			return nil, err
		}
		if c.UserAgent != "" {
			req.Header.Set("User-Agent", c.UserAgent)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("unexpected status code: %d, %v", resp.StatusCode, string(data))
		}

		var apiResp GetArticleResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
			return nil, err
		}

		articles = append(articles, apiResp.Query.AllPages...)

		if apiResp.Continue == nil || apiResp.Continue.Continue == "" || len(articles) >= limit {
			break
		}
		continueToken = apiResp.Continue
	}

	return articles, nil
}
