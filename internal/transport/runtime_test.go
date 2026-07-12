// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package transport

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamableHTTPRuntime(t *testing.T) {
	testHTTPRuntime(t, ModeStreamableHTTP)
}

func TestLegacySSERuntime(t *testing.T) {
	testHTTPRuntime(t, ModeSSE)
}

func testHTTPRuntime(t *testing.T, mode Mode) {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "transport-test", Version: "test"}, nil)
	endpointPath := "/mcp"
	if mode == ModeSSE {
		endpointPath = "/sse"
	}
	runtime := NewRuntime(server, Options{
		Mode:           mode,
		HTTPAddress:    "127.0.0.1:0",
		HTTPPath:       endpointPath,
		SessionTimeout: time.Minute,
	})
	require.NoError(t, runtime.Prepare())

	runDone := make(chan error, 1)
	serverContext, cancelServer := context.WithCancel(context.Background())
	go func() { runDone <- runtime.Run(serverContext) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "transport-test-client", Version: "test"}, nil)
	var clientTransport mcp.Transport
	if mode == ModeStreamableHTTP {
		clientTransport = &mcp.StreamableClientTransport{Endpoint: runtime.Endpoint()}
	} else {
		clientTransport = &mcp.SSEClientTransport{Endpoint: runtime.Endpoint()}
	}
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)

	tools, err := clientSession.ListTools(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, tools.Tools)
	require.NoError(t, clientSession.Close())

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelShutdown()
	require.NoError(t, runtime.Shutdown(shutdownContext))
	cancelServer()
	require.NoError(t, <-runDone)
}
