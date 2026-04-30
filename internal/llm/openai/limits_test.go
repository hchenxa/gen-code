package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/openai/openai-go/v3"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestParseOpenAIModelDocLimits(t *testing.T) {
	body := []byte(`
		<html><body>
			<section>
				<p>400,000 context window</p>
				<p>128,000 max output tokens</p>
			</section>
		</body></html>
	`)

	limits, err := parseOpenAIModelDocLimits(body)
	if err != nil {
		t.Fatalf("parseOpenAIModelDocLimits() error = %v", err)
	}
	if limits.input != 400000 || limits.output != 128000 {
		t.Fatalf("limits = input:%d output:%d, want input:400000 output:128000", limits.input, limits.output)
	}
}

func TestFetchModelLimitsUsesOpenAIModelDocs(t *testing.T) {
	var requestedPath string
	withOpenAIModelDocsHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		requestedPath = r.URL.Path
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`
			<html><body>
				<p>1,050,000 context window</p>
				<p>128,000 max output tokens</p>
			</body></html>
		`)),
			Request: r,
		}, nil
	})

	oldBaseURL := openAIModelDocsBaseURL
	openAIModelDocsBaseURL = "https://example.test/"
	defer func() { openAIModelDocsBaseURL = oldBaseURL }()

	client := NewClient(sdk.Client{}, "openai:test")
	input, output, err := client.FetchModelLimits(context.Background(), "gpt-5.4")
	if err != nil {
		t.Fatalf("FetchModelLimits() error = %v", err)
	}
	if input != 1050000 || output != 128000 {
		t.Fatalf("limits = input:%d output:%d, want input:1050000 output:128000", input, output)
	}
	if requestedPath != "/gpt-5.4" {
		t.Fatalf("requested path = %q, want /gpt-5.4", requestedPath)
	}
}

func TestFetchModelLimitsFallsBackFromSnapshotToBaseModelDoc(t *testing.T) {
	var requestedPaths []string
	withOpenAIModelDocsHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		if r.URL.Path == "/gpt-5.4-mini-2026-03-17" {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(`
			<html><body>
				<p>400,000 context window</p>
				<p>128,000 max output tokens</p>
			</body></html>
		`)),
			Request: r,
		}, nil
	})

	oldBaseURL := openAIModelDocsBaseURL
	openAIModelDocsBaseURL = "https://example.test/"
	defer func() { openAIModelDocsBaseURL = oldBaseURL }()

	client := NewClient(sdk.Client{}, "openai:test")
	input, output, err := client.FetchModelLimits(context.Background(), "gpt-5.4-mini-2026-03-17")
	if err != nil {
		t.Fatalf("FetchModelLimits() error = %v", err)
	}
	if input != 400000 || output != 128000 {
		t.Fatalf("limits = input:%d output:%d, want input:400000 output:128000", input, output)
	}
	if len(requestedPaths) != 2 || requestedPaths[0] != "/gpt-5.4-mini-2026-03-17" || requestedPaths[1] != "/gpt-5.4-mini" {
		t.Fatalf("requested paths = %#v, want snapshot then base model", requestedPaths)
	}
}

func withOpenAIModelDocsHTTPClient(t *testing.T, fn func(*http.Request) (*http.Response, error)) {
	t.Helper()
	oldClient := openAIModelDocsHTTPClient
	openAIModelDocsHTTPClient = &http.Client{Transport: roundTripFunc(fn)}
	t.Cleanup(func() { openAIModelDocsHTTPClient = oldClient })
}
