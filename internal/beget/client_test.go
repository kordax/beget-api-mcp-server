package beget

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientUsesPOSTAndUnwrapsAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/domain/getList" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if request.Form.Get("login") != "test-login" || request.Form.Get("passwd") != "test-key" {
			t.Fatal("credentials were not sent in the POST form")
		}
		if request.URL.RawQuery != "" {
			t.Fatal("credentials must not be placed in the URL")
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"status":"success","answer":[{"fqdn":"example.com"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/api", "test-login", "test-key", server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	answer, err := client.Call(context.Background(), "domain", "getList", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var domains []map[string]any
	if err := json.Unmarshal(answer, &domains); err != nil || len(domains) != 1 {
		t.Fatalf("unexpected answer: %s (%v)", answer, err)
	}
}

func TestClientReturnsSanitizedAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte(`{"status":"error","error_code":7,"error_text":"denied"}`))
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, "test-login", "secret-that-must-not-leak", server.Client())
	_, err := client.Call(context.Background(), "user", "getAccountInfo", nil)
	var apiError *APIError
	if !errors.As(err, &apiError) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if strings.Contains(err.Error(), "secret-that-must-not-leak") {
		t.Fatal("API key leaked into the error")
	}
}

func TestClientRejectsArbitraryPaths(t *testing.T) {
	client, _ := NewClient("https://example.invalid/api", "login", "key", nil)
	if _, err := client.Call(context.Background(), "../user", "getAccountInfo", nil); err == nil {
		t.Fatal("expected invalid section to fail")
	}
}
