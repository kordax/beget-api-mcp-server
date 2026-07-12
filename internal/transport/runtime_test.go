// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package transport

import (
	"context"
	"errors"
	"net"
	"net/http"
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

func TestRuntimeGuardsAndAccessors(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "transport-test", Version: "test"}, nil)
	stdio := NewRuntime(server, Options{Mode: ModeStdio})
	require.NoError(t, stdio.Prepare())
	assert.Empty(t, stdio.Endpoint())
	assert.Equal(t, ModeStdio, stdio.Mode())
	require.NoError(t, stdio.Shutdown(context.Background()))

	notPrepared := NewRuntime(server, Options{Mode: ModeSSE})
	assert.ErrorContains(t, notPrepared.Run(context.Background()), "was not prepared")

	invalidAddress := NewRuntime(server, Options{Mode: ModeSSE, HTTPAddress: "127.0.0.1:-1", HTTPPath: "/sse"})
	assert.ErrorContains(t, invalidAddress.Prepare(), "listen on")

	unsupported := NewRuntime(server, Options{Mode: Mode("unsupported"), HTTPAddress: "127.0.0.1:0", HTTPPath: "/mcp"})
	assert.ErrorContains(t, unsupported.Prepare(), "unsupported transport mode")
	assert.Empty(t, unsupported.Endpoint())

	prepared := NewRuntime(server, Options{Mode: ModeSSE, HTTPAddress: "127.0.0.1:0", HTTPPath: "/sse"})
	require.NoError(t, prepared.Prepare())
	assert.NotEmpty(t, prepared.Endpoint())
	assert.ErrorContains(t, prepared.Prepare(), "already prepared")
	require.NoError(t, prepared.Shutdown(context.Background()))
}

func TestRuntimePropagatesServeError(t *testing.T) {
	expected := errors.New("accept failed")
	runtime := &Runtime{
		options:    Options{Mode: ModeSSE},
		httpServer: &http.Server{},
		listener:   failingListener{err: expected},
	}

	assert.ErrorIs(t, runtime.Run(context.Background()), expected)
}

type failingListener struct {
	err error
}

func (listener failingListener) Accept() (net.Conn, error) { return nil, listener.err }
func (failingListener) Close() error                       { return nil }
func (failingListener) Addr() net.Addr                     { return testAddress("failure") }

type testAddress string

func (address testAddress) Network() string { return string(address) }
func (address testAddress) String() string  { return string(address) }

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
		httpTransport := &http.Transport{DisableKeepAlives: true}
		t.Cleanup(httpTransport.CloseIdleConnections)
		clientTransport = &mcp.StreamableClientTransport{
			Endpoint:             runtime.Endpoint(),
			HTTPClient:           &http.Client{Transport: httpTransport},
			DisableStandaloneSSE: true,
		}
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
