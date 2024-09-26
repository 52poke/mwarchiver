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
}

// NewClient creates a new MediaWiki client.
func NewClient(apiURL string) *Client {
	return &Client{
		APIURL: apiURL,
	}
}

type GetPageResponse struct {
	Query struct {
		Pages map[string]struct {
			Revisions []struct {
				Slots struct {
					Main struct {
						Content string `json:"*"`
					} `json:"main"`
				} `json:"slots"`
			} `json:"revisions"`
		} `json:"pages"`
	} `json:"query"`
}

// GetPage gets a page from the MediaWiki API.
func (c *Client) GetPage(title string) (string, error) {
	slog.Info("Getting page", slog.String("title", title))

	params := fmt.Sprintf("action=query&format=json&titles=%s&prop=revisions&rvlimit=1&rvprop=content&rvslots=*", url.QueryEscape(title))
	resp, err := http.Get(c.APIURL + "?" + params)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result GetPageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	for _, page := range result.Query.Pages {
		if len(page.Revisions) > 0 {
			return page.Revisions[0].Slots.Main.Content, nil
		}
	}

	return "", fmt.Errorf("no wikitext found for title: %s", title)
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
		resp, err := http.Get(c.APIURL + "?" + params)
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
