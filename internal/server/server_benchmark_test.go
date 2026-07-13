// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func BenchmarkServerStartup(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = newServer(&fakeCaller{}, nil, time.Now)
	}
}

func BenchmarkMCPInitialize(b *testing.B) {
	server := newServer(&fakeCaller{}, nil, time.Now)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		session, closeSessions := connectBenchmarkClient(b, server)
		if session.InitializeResult() == nil {
			b.Fatal("initialize result is nil")
		}
		closeSessions()
	}
}

func BenchmarkMCPToolsList(b *testing.B) {
	session, closeSessions := connectBenchmarkClient(b, newServer(&fakeCaller{}, nil, time.Now))
	defer closeSessions()
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		b.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := session.ListTools(context.Background(), nil); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(len(encoded)), "response-bytes")
	b.ReportMetric(float64(len(result.Tools)), "tools")
}

func BenchmarkInputSchemaGeneration(b *testing.B) {
	b.Run("largest-cron-input", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = mustInputSchema[CronAddInput]()
		}
	})
	b.Run("nested-directives-input", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = mustInputSchema[DirectivesInput]()
		}
	})
}

func BenchmarkMCPToolCall(b *testing.B) {
	session, closeSessions := connectBenchmarkClient(b, newServer(&fakeCaller{}, nil, time.Now))
	defer closeSessions()
	params := &mcp.CallToolParams{Name: "beget_auth_status", Arguments: map[string]any{}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := session.CallTool(context.Background(), params); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMCPConcurrentToolCalls(b *testing.B) {
	session, closeSessions := connectBenchmarkClient(b, newServer(&fakeCaller{}, nil, time.Now))
	defer closeSessions()
	params := &mcp.CallToolParams{Name: "beget_auth_status", Arguments: map[string]any{}}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			if _, err := session.CallTool(context.Background(), params); err != nil {
				b.Error(err)
			}
		}
	})
}

func connectBenchmarkClient(b *testing.B, server *mcp.Server) (*mcp.ClientSession, func()) {
	b.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		b.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "benchmark-client", Version: "benchmark"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		b.Fatal(err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
}
