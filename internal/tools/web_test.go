package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/search" {
			t.Fatalf("path = %q, want /search", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization header was not set")
		}

		var body webSearchRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Query != "go agents" || body.MaxResults != 3 || body.SearchDepth != "basic" {
			t.Fatalf("unexpected request body: %+v", body)
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"results":[{"title":"Go","url":"https://go.dev","content":"The Go website.","score":0.9}]}`))
	}))
	defer server.Close()

	restoreTavilyTestState(t, server.URL)
	result, err := WebSearch([]byte(`{"query":"go agents","max_results":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"url":"https://go.dev"`) {
		t.Fatalf("result = %s", result)
	}
}

func TestWebFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/extract" {
			t.Fatalf("path = %q, want /extract", request.URL.Path)
		}

		var body webFetchRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.URLs != "https://example.com/page" || body.Format != "markdown" {
			t.Fatalf("unexpected request body: %+v", body)
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"results":[{"url":"https://example.com/page","raw_content":"# example\npage text"}],"failed_results":[]}`))
	}))
	defer server.Close()

	restoreTavilyTestState(t, server.URL)
	result, err := WebFetch([]byte(`{"url":"https://example.com/page"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"# example\npage text"`) {
		t.Fatalf("result = %s", result)
	}
}

func TestWebToolsRequireTavilyAPIKey(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "")
	_, err := WebSearch([]byte(`{"query":"test"}`))
	if err == nil || !strings.Contains(err.Error(), "missing TAVILY_API_KEY") {
		t.Fatalf("error = %v", err)
	}
}

func restoreTavilyTestState(t *testing.T, baseURL string) {
	t.Helper()
	originalBaseURL := tavilyBaseURL
	tavilyBaseURL = baseURL
	t.Cleanup(func() {
		tavilyBaseURL = originalBaseURL
	})
	t.Setenv("TAVILY_API_KEY", "test-key")
}
