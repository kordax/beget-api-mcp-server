// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package beget

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkClientHTTPRoundTrip(b *testing.B) {
	client := newBenchmarkClient(b)
	input := struct {
		Domain string `json:"domain"`
	}{Domain: "example.test"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := client.Call(context.Background(), "mail", "getMailboxList", input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClientConcurrentHTTPRoundTrips(b *testing.B) {
	client := newBenchmarkClient(b)
	input := struct {
		Domain string `json:"domain"`
	}{Domain: "example.test"}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			if _, err := client.Call(context.Background(), "mail", "getMailboxList", input); err != nil {
				b.Error(err)
			}
		}
	})
}

func newBenchmarkClient(b *testing.B) *Client {
	b.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"status":"success","answer":[{"mailbox":"admin","domain":"example.test"}]}`))
	}))
	b.Cleanup(server.Close)
	client, err := NewClient(server.URL, "benchmark-login", "benchmark-api-key", server.Client())
	if err != nil {
		b.Fatal(err)
	}
	return client
}
