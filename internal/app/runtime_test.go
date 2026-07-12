// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kordax/beget-api-mcp-server/internal/transport"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

type lifecycleRecorder struct {
	hook fx.Hook
}

func (recorder *lifecycleRecorder) Append(hook fx.Hook) {
	recorder.hook = hook
}

type shutdownRecorder struct {
	called chan struct{}
	err    error
}

func (recorder *shutdownRecorder) Shutdown(...fx.ShutdownOption) error {
	select {
	case recorder.called <- struct{}{}:
	default:
	}
	return recorder.err
}

func TestRegisterLifecycleStartsAndStopsHTTPRuntime(t *testing.T) {
	lifecycle := &lifecycleRecorder{}
	shutdowner := &shutdownRecorder{called: make(chan struct{}, 1), err: errors.New("shutdown signal failed")}
	server := mcp.NewServer(&mcp.Implementation{Name: "app-test", Version: "test"}, nil)
	runtime := transport.NewRuntime(server, transport.Options{
		Mode:        transport.ModeStreamableHTTP,
		HTTPAddress: "127.0.0.1:0",
		HTTPPath:    "/mcp",
	})

	RegisterLifecycle(lifecycle, shutdowner, runtime)
	require.NotNil(t, lifecycle.hook.OnStart)
	require.NotNil(t, lifecycle.hook.OnStop)
	require.NoError(t, lifecycle.hook.OnStart(context.Background()))
	assert.NotEmpty(t, runtime.Endpoint())

	stopContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, lifecycle.hook.OnStop(stopContext))

	select {
	case <-shutdowner.called:
	case <-time.After(time.Second):
		t.Fatal("lifecycle did not request application shutdown")
	}
}

func TestRegisterLifecycleReturnsPrepareAndStopErrors(t *testing.T) {
	t.Run("prepare", func(t *testing.T) {
		lifecycle := &lifecycleRecorder{}
		shutdowner := &shutdownRecorder{called: make(chan struct{}, 1)}
		server := mcp.NewServer(&mcp.Implementation{Name: "app-test", Version: "test"}, nil)
		runtime := transport.NewRuntime(server, transport.Options{
			Mode:        transport.ModeSSE,
			HTTPAddress: "127.0.0.1:-1",
			HTTPPath:    "/sse",
		})

		RegisterLifecycle(lifecycle, shutdowner, runtime)
		assert.ErrorContains(t, lifecycle.hook.OnStart(context.Background()), "listen on")
	})

	t.Run("stop before start", func(t *testing.T) {
		lifecycle := &lifecycleRecorder{}
		shutdowner := &shutdownRecorder{called: make(chan struct{}, 1)}
		server := mcp.NewServer(&mcp.Implementation{Name: "app-test", Version: "test"}, nil)
		runtime := transport.NewRuntime(server, transport.Options{Mode: transport.ModeStdio})

		RegisterLifecycle(lifecycle, shutdowner, runtime)
		stopContext, cancel := context.WithCancel(context.Background())
		cancel()
		assert.ErrorIs(t, lifecycle.hook.OnStop(stopContext), context.Canceled)
	})
}
