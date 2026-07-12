package config

import "testing"

func TestFromEnvironment(t *testing.T) {
	t.Setenv("BEGET_API_LOGIN", "account")
	t.Setenv("BEGET_API_KEY", "test-only-secret")
	t.Setenv("BEGET_API_BASE_URL", "https://example.invalid/api/")

	config, err := FromEnvironment()
	if err != nil {
		t.Fatalf("FromEnvironment returned an error: %v", err)
	}
	if config.Login != "account" || config.APIKey != "test-only-secret" {
		t.Fatalf("unexpected credentials: login=%q key-present=%t", config.Login, config.APIKey != "")
	}
	if config.BaseURL != "https://example.invalid/api" {
		t.Fatalf("unexpected base URL: %q", config.BaseURL)
	}
}

func TestFromEnvironmentRequiresCredentials(t *testing.T) {
	t.Setenv("BEGET_API_LOGIN", "")
	t.Setenv("BEGET_API_KEY", "")

	if _, err := FromEnvironment(); err == nil {
		t.Fatal("expected missing credentials to fail")
	}
}
