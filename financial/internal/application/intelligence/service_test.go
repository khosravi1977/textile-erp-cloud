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
}

func TestGenerateRequiresServerConfiguration(t *testing.T) {
	if _, err := New(nil, Config{}, nil).Generate(context.Background(), 1, 1, Summary{}); !errorsIs(err, ErrDisabled) {
		t.Fatalf("expected disabled error, got %v", err)
	}
	if _, err := New(nil, Config{Enabled: true}, nil).Generate(context.Background(), 1, 1, Summary{}); !errorsIs(err, ErrNotConfigured) {
		t.Fatalf("expected not configured error, got %v", err)
	}
}

func errorsIs(err, target error) bool {
	return err != nil && strings.Contains(err.Error(), target.Error())
}
