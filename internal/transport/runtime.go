// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package transport

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Runtime struct {
	mcpServer  *mcp.Server
	options    Options
	listener   net.Listener
	httpServer *http.Server
}

func NewRuntime(mcpServer *mcp.Server, options Options) *Runtime {
	return &Runtime{mcpServer: mcpServer, options: options}
}

func (r *Runtime) Prepare() error {
	if r.options.Mode == ModeStdio {
		return nil
	}
	if r.listener != nil {
		return errors.New("transport runtime is already prepared")
	}

	listener, err := net.Listen("tcp", r.options.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", r.options.HTTPAddress, err)
	}
	r.listener = listener

	var handler http.Handler
	switch r.options.Mode {
	case ModeStreamableHTTP:
		handler = mcp.NewStreamableHTTPHandler(
			func(*http.Request) *mcp.Server { return r.mcpServer },
			&mcp.StreamableHTTPOptions{
				Stateless:      r.options.StreamableStateless,
				JSONResponse:   r.options.JSONResponse,
				SessionTimeout: r.options.SessionTimeout,
			},
		)
	case ModeSSE:
		handler = mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return r.mcpServer }, nil)
	default:
		_ = listener.Close()
		r.listener = nil
		return fmt.Errorf("unsupported transport mode %q", r.options.Mode)
	}

	protection := http.NewCrossOriginProtection()
	if r.options.HTTPBearerToken != "" {
		handler = requireBearerToken(r.options.HTTPBearerToken, handler)
	}
	mux := http.NewServeMux()
	mux.Handle(r.options.HTTPPath, protection.Handler(handler))
	r.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	return nil
}

func (r *Runtime) Run(ctx context.Context) error {
	if r.options.Mode == ModeStdio {
		return r.mcpServer.Run(ctx, &mcp.StdioTransport{})
	}
	if r.httpServer == nil || r.listener == nil {
		return errors.New("HTTP transport runtime was not prepared")
	}
	if err := r.httpServer.Serve(r.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r.httpServer == nil {
		return nil
	}
	return r.httpServer.Shutdown(ctx)
}

func (r *Runtime) Endpoint() string {
	if r.listener == nil {
		return ""
	}
	return "http://" + r.listener.Addr().String() + r.options.HTTPPath
}

func (r *Runtime) Mode() Mode {
	return r.options.Mode
}

func requireBearerToken(token string, next http.Handler) http.Handler {
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		provided := []byte(strings.TrimSpace(request.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(provided, expected) != 1 {
			response.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(response, request)
	})
}
