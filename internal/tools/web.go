package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultTavilyBaseURL = "https://api.tavily.com"
	maxTavilyResponse    = 8 << 20
	maxFetchedContent    = 30_000
)

var tavilyBaseURL = defaultTavilyBaseURL

var tavilyHTTPClient = &http.Client{
	Timeout: 35 * time.Second,
}

type WebSearchInput struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Topic      string `json:"topic"`
}

type webSearchRequest struct {
	Query       string `json:"query"`
	SearchDepth string `json:"search_depth"`
	MaxResults  int    `json:"max_results"`
	Topic       string `json:"topic"`
}

type webSearchResponse struct {
	Results []WebSearchResult `json:"results"`
}

type WebSearchResult struct {
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Content       string  `json:"content"`
	Score         float64 `json:"score,omitempty"`
	PublishedDate string  `json:"published_date,omitempty"`
}

func WebSearch(ctx context.Context, input json.RawMessage) (string, error) {
	var args WebSearchInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid web_search input: %w", err)
	}

	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if args.MaxResults == 0 {
		args.MaxResults = 5
	}
	if args.MaxResults < 1 || args.MaxResults > 10 {
		return "", fmt.Errorf("max_results must be between 1 and 10")
	}
	if args.Topic == "" {
		args.Topic = "general"
	}
	if args.Topic != "general" && args.Topic != "news" && args.Topic != "finance" {
		return "", fmt.Errorf("topic must be general, news, or finance")
	}

	request := webSearchRequest{
		Query:       args.Query,
		SearchDepth: "basic",
		MaxResults:  args.MaxResults,
		Topic:       args.Topic,
	}
	var response webSearchResponse
	if err := callTavily(ctx, "/search", request, &response); err != nil {
		return "", err
	}

	result, err := json.Marshal(response.Results)
	if err != nil {
		return "", fmt.Errorf("failed to encode search results: %w", err)
	}
	return string(result), nil
}

type WebFetchInput struct {
	URL string `json:"url"`
}

type webFetchRequest struct {
	URLs         string `json:"urls"`
	ExtractDepth string `json:"extract_depth"`
	Format       string `json:"format"`
}

type webFetchResponse struct {
	Results       []webFetchResult  `json:"results"`
	FailedResults []json.RawMessage `json:"failed_results"`
}

type webFetchResult struct {
	URL        string `json:"url"`
	RawContent string `json:"raw_content"`
}

type WebFetchResult struct {
	URL       string `json:"url"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

func WebFetch(ctx context.Context, input json.RawMessage) (string, error) {
	var args WebFetchInput
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("invalid web_fetch input: %w", err)
	}

	args.URL = strings.TrimSpace(args.URL)
	parsedURL, err := url.ParseRequestURI(args.URL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return "", fmt.Errorf("url must be a valid http or https URL")
	}

	request := webFetchRequest{
		URLs:         args.URL,
		ExtractDepth: "basic",
		Format:       "markdown",
	}
	var response webFetchResponse
	if err := callTavily(ctx, "/extract", request, &response); err != nil {
		return "", err
	}
	if len(response.Results) == 0 {
		if len(response.FailedResults) > 0 {
			failures, _ := json.Marshal(response.FailedResults)
			return "", fmt.Errorf("tavily could not fetch the URL: %s", failures)
		}
		return "", fmt.Errorf("tavily returned no content for the URL")
	}

	content := response.Results[0].RawContent
	truncated := len(content) > maxFetchedContent
	if truncated {
		content = content[:maxFetchedContent]
	}

	result, err := json.Marshal(WebFetchResult{
		URL:       response.Results[0].URL,
		Content:   content,
		Truncated: truncated,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode fetched content: %w", err)
	}
	return string(result), nil
}

func callTavily(ctx context.Context, path string, requestBody any, responseBody any) error {
	apiKey := strings.TrimSpace(os.Getenv("TAVILY_API_KEY"))
	if apiKey == "" {
		return fmt.Errorf("missing TAVILY_API_KEY; add it to .env")
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to encode tavily request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilyBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create tavily request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := tavilyHTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("tavily request failed: %w", err)
	}
	defer response.Body.Close()

	responseData, err := io.ReadAll(io.LimitReader(response.Body, maxTavilyResponse))
	if err != nil {
		return fmt.Errorf("failed to read tavily response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("tavily returned %s: %s", response.Status, strings.TrimSpace(string(responseData)))
	}
	if err := json.Unmarshal(responseData, responseBody); err != nil {
		return fmt.Errorf("failed to decode tavily response: %w", err)
	}
	return nil
}
