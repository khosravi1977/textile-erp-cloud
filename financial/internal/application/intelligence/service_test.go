package intelligence

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestGenerateParsesResponsesAPIAndDoesNotExposeKey(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://example.test/v1/responses" {
			t.Fatalf("unexpected URL: %s", request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer secret-test" {
			t.Fatal("missing server-side authorization")
		}
		body, _ := io.ReadAll(request.Body)
		if strings.Contains(string(body), "secret-test") {
			t.Fatal("API key leaked into request body")
		}
		payload := `{"output":[{"type":"message","content":[{"type":"output_text","text":"{\"executive_summary\":\"خلاصه\",\"highlights\":[\"فروش\"],\"risks\":[\"نقدینگی\"],\"recommended_focus\":\"وصول\"}"}]}],"usage":{"input_tokens":12,"output_tokens":8,"total_tokens":20}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header)}, nil
	})}
	service := New(nil, Config{Enabled: true, APIKey: "secret-test", BaseURL: "https://example.test/v1", Model: "test-model"}, client)
	result, err := service.Generate(context.Background(), 10, 20, Summary{PeriodMonths: 3, HealthScore: 70})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Narrative.ExecutiveSummary != "خلاصه" || result.TotalTokens != 20 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Mode != "provider" {
		t.Fatalf("expected provider mode, got %q", result.Mode)
	}
}

func TestGenerateSupportsOpenAICompatibleChatCompletions(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://api.deepseek.test/v1/chat/completions" {
			t.Fatalf("unexpected URL: %s", request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer deepseek-secret" {
			t.Fatal("missing server-side authorization")
		}
		body, _ := io.ReadAll(request.Body)
		if strings.Contains(string(body), "deepseek-secret") {
			t.Fatal("API key leaked into request body")
		}
		payload := `{"choices":[{"message":{"content":"{\"executive_summary\":\"خلاصه مالی\",\"highlights\":[\"فروش\"],\"risks\":[\"نقدینگی\"],\"recommended_focus\":\"وصول مطالبات\"}"}}],"usage":{"prompt_tokens":21,"completion_tokens":13,"total_tokens":34}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header)}, nil
	})}
	service := New(nil, Config{
		Enabled: true, APIKey: "deepseek-secret", BaseURL: "https://api.deepseek.test/v1",
		Model: "deepseek-chat", APIStyle: "chat_completions",
	}, client)
	result, err := service.Generate(context.Background(), 10, 20, Summary{PeriodMonths: 3, HealthScore: 70})
	if err != nil {
		t.Fatalf("generate chat completion: %v", err)
	}
	if result.Narrative.ExecutiveSummary != "خلاصه مالی" || result.TotalTokens != 34 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Mode != "provider" {
		t.Fatalf("expected provider mode, got %q", result.Mode)
	}
}

func TestGenerateUsesLocalAdvisorWithoutProviderConfiguration(t *testing.T) {
	result, err := New(nil, Config{}, nil).Generate(context.Background(), 1, 1, Summary{
		PeriodMonths:     3,
		HealthScore:      62,
		DataCompleteness: 80,
		Revenue:          200,
		Expenses:         260,
		CustomerDebt:     90,
	})
	if err != nil {
		t.Fatalf("local advisor failed: %v", err)
	}
	if result.Mode != "local" || result.Model != "viora-local-advisor-v1" {
		t.Fatalf("unexpected local advisor identity: %#v", result)
	}
	if strings.TrimSpace(result.Narrative.ExecutiveSummary) == "" ||
		len(result.Narrative.Highlights) == 0 ||
		len(result.Narrative.Risks) == 0 ||
		strings.TrimSpace(result.Narrative.RecommendedFocus) == "" {
		t.Fatalf("local advisor returned incomplete guidance: %#v", result.Narrative)
	}
	if result.TotalTokens != 0 {
		t.Fatalf("local advisor must not report provider token usage: %#v", result)
	}
}

func TestGenerateFallsBackLocallyWhenProviderIsUnavailable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})}
	result, err := New(nil, Config{
		Enabled: true,
		APIKey:  "secret-test",
		BaseURL: "https://example.test/v1",
		Model:   "test-model",
	}, client).Generate(context.Background(), 1, 1, Summary{PeriodMonths: 1, HealthScore: 70})
	if err != nil {
		t.Fatalf("provider fallback failed: %v", err)
	}
	if result.Mode != "local-fallback" || result.Model != "viora-local-advisor-v1" {
		t.Fatalf("expected local fallback, got %#v", result)
	}
}
