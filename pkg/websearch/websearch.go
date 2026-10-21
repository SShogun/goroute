package websearch

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type FetchRequest struct {
	URL             string `json:"url"`
	Provider        string `json:"provider,omitempty"`
	Format          string `json:"format,omitempty"` // "markdown", "text", "html"
	Depth           int    `json:"depth,omitempty"`
	WaitForSelector string `json:"wait_for_selector,omitempty"`
	IncludeMetadata bool   `json:"include_metadata,omitempty"`
}

type FetchMetadata struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Language    string `json:"language,omitempty"`
	StatusCode  int    `json:"statusCode,omitempty"`
}

type FetchResponse struct {
	Provider      string         `json:"provider"`
	URL           string         `json:"url"`
	Content       string         `json:"content"`
	Links         []string       `json:"links,omitempty"`
	Metadata      *FetchMetadata `json:"metadata,omitempty"`
	ScreenshotURL string         `json:"screenshot_url,omitempty"`
}

type MapRequest struct {
	URL                   string `json:"url"`
	Limit                 int    `json:"limit,omitempty"`
	Search                string `json:"search,omitempty"`
	Sitemap               string `json:"sitemap,omitempty"`
	IncludeSubdomains     bool   `json:"includeSubdomains,omitempty"`
	IgnoreQueryParameters bool   `json:"ignoreQueryParameters,omitempty"`
}

type MapResponse struct {
	Success bool     `json:"success"`
	Links   []string `json:"links"`
}

type SearchRequest struct {
	Query      string `json:"query"`
	Provider   string `json:"provider,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
	SearchType string `json:"search_type,omitempty"`
}

type SearchResultItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Content string `json:"content,omitempty"`
}

type SearchResponse struct {
	Query    string             `json:"query"`
	Provider string             `json:"provider"`
	Results  []SearchResultItem `json:"results"`
	Total    int                `json:"total"`
}

var (
	titleRegex   = regexp.MustCompile(`(?i)<title[^>]*>([\s\S]*?)</title>`)
	metaDescRe   = regexp.MustCompile(`(?i)<meta[^>]+name=["']description["'][^>]+content=["']([^"']*)["']`)
	scriptRegex  = regexp.MustCompile(`(?i)<script[\s\S]*?</script>|<style[\s\S]*?</style>|<svg[\s\S]*?</svg>|<noscript[\s\S]*?</noscript>`)
	linkRegex    = regexp.MustCompile(`(?i)<a[^>]+href=["']([^"']+)["'][^>]*>([\s\S]*?)</a>`)
	tagRegex     = regexp.MustCompile(`<[^>]+>`)
	wsRegex      = regexp.MustCompile(`[ \t]+`)
	newlineRegex = regexp.MustCompile(`\n{3,}`)
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36 GoRoute/1.0"

// Fetch extracts readable content and metadata from a target web URL.
func Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if req.URL == "" {
		return nil, fmt.Errorf("url parameter is required")
	}

	parsedURL, err := url.Parse(req.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return nil, fmt.Errorf("invalid http/https url: %s", req.URL)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", userAgent)
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("web fetch failed: %w", err)
	}
	defer resp.Body.Close()

	// Read up to 2MB to prevent memory exhaustion
	limitReader := io.LimitReader(resp.Body, 2*1024*1024)
	bodyBytes, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	rawHTML := string(bodyBytes)

	// Extract Title and Meta description
	var meta FetchMetadata
	meta.StatusCode = resp.StatusCode
	if match := titleRegex.FindStringSubmatch(rawHTML); len(match) > 1 {
		meta.Title = strings.TrimSpace(html.UnescapeString(match[1]))
	}
	if match := metaDescRe.FindStringSubmatch(rawHTML); len(match) > 1 {
		meta.Description = strings.TrimSpace(html.UnescapeString(match[1]))
	}

	// Extract outbound links
	linksFound := extractLinks(rawHTML, parsedURL)

	// Format content
	var content string
	switch req.Format {
	case "html":
		content = rawHTML
	default:
		// Convert HTML to clean readable text/markdown
		clean := scriptRegex.ReplaceAllString(rawHTML, " ")
		clean = tagRegex.ReplaceAllString(clean, "\n")
		clean = html.UnescapeString(clean)

		var lines []string
		for _, line := range strings.Split(clean, "\n") {
			l := strings.TrimSpace(wsRegex.ReplaceAllString(line, " "))
			if l != "" {
				lines = append(lines, l)
			}
		}
		content = newlineRegex.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
		if meta.Title != "" {
			content = "# " + meta.Title + "\n\n" + content
		}
	}

	return &FetchResponse{
		Provider: "direct",
		URL:      req.URL,
		Content:  content,
		Links:    linksFound,
		Metadata: &meta,
	}, nil
}

// Map extracts links and sitemap information from a web domain.
func Map(ctx context.Context, req MapRequest) (*MapResponse, error) {
	if req.URL == "" {
		return nil, fmt.Errorf("url parameter is required")
	}

	parsedURL, err := url.Parse(req.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}

	limit := req.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	fetchResp, err := Fetch(ctx, FetchRequest{URL: req.URL, Format: "html"})
	if err != nil {
		return nil, err
	}

	var matched []string
	seen := make(map[string]bool)

	for _, link := range fetchResp.Links {
		u, err := url.Parse(link)
		if err != nil {
			continue
		}
		if !req.IncludeSubdomains && u.Host != parsedURL.Host {
			continue
		}
		if req.Search != "" && !strings.Contains(strings.ToLower(link), strings.ToLower(req.Search)) {
			continue
		}
		normalized := link
		if req.IgnoreQueryParameters {
			u.RawQuery = ""
			normalized = u.String()
		}
		if !seen[normalized] {
			seen[normalized] = true
			matched = append(matched, normalized)
			if len(matched) >= limit {
				break
			}
		}
	}

	return &MapResponse{
		Success: true,
		Links:   matched,
	}, nil
}

var (
	ddgAnchorRe  = regexp.MustCompile(`(?i)<a\b[^>]*?class=['"][^'"]*result-link[^'"]*['"][^>]*href=['"]([^'"]+)['"][^>]*>([\s\S]*?)</a>`)
	ddgSnippetRe = regexp.MustCompile(`(?i)<td\b[^>]*?class=['"][^'"]*result-snippet[^'"]*['"][^>]*>([\s\S]*?)</td>`)
)

// Search conducts web search using DuckDuckGo lite HTML parser with zero API key dependencies.
func Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("query parameter is required")
	}

	maxResults := req.MaxResults
	if maxResults <= 0 || maxResults > 50 {
		maxResults = 10
	}

	endpoint := "https://lite.duckduckgo.com/lite/"
	data := url.Values{}
	data.Set("q", req.Query)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		// Return graceful fallback
		return &SearchResponse{
			Query:    req.Query,
			Provider: "duckduckgo-free",
			Results:  []SearchResultItem{},
			Total:    0,
		}, nil
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	body := string(bodyBytes)

	anchors := ddgAnchorRe.FindAllStringSubmatch(body, -1)
	snippets := ddgSnippetRe.FindAllStringSubmatch(body, -1)

	var items []SearchResultItem
	for i := 0; i < len(anchors) && i < maxResults; i++ {
		rawURL := anchors[i][1]
		title := stripTags(html.UnescapeString(anchors[i][2]))

		// Decode DDG redirect URL if necessary
		if strings.Contains(rawURL, "duckduckgo.com/l/?uddg=") {
			if u, err := url.Parse(rawURL); err == nil {
				if actual := u.Query().Get("uddg"); actual != "" {
					rawURL = actual
				}
			}
		}

		snippet := ""
		if i < len(snippets) {
			snippet = stripTags(html.UnescapeString(snippets[i][1]))
		}

		items = append(items, SearchResultItem{
			Title:   title,
			URL:     rawURL,
			Snippet: snippet,
			Content: snippet,
		})
	}

	return &SearchResponse{
		Query:    req.Query,
		Provider: "duckduckgo-free",
		Results:  items,
		Total:    len(items),
	}, nil
}

func extractLinks(rawHTML string, base *url.URL) []string {
	matches := linkRegex.FindAllStringSubmatch(rawHTML, -1)
	var links []string
	seen := make(map[string]bool)

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		href := strings.TrimSpace(m[1])
		if href == "" || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "mailto:") {
			continue
		}

		u, err := url.Parse(href)
		if err != nil {
			continue
		}
		resolved := base.ResolveReference(u).String()
		if !seen[resolved] {
			seen[resolved] = true
			links = append(links, resolved)
		}
	}
	return links
}

func stripTags(s string) string {
	s = tagRegex.ReplaceAllString(s, " ")
	s = wsRegex.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
