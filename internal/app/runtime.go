// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package app

import (
	"context"
	"errors"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
)

func RegisterLifecycle(lifecycle fx.Lifecycle, shutdowner fx.Shutdowner, mcpServer *mcp.Server) {
	serverContext, cancelServer := context.WithCancel(context.Background())
	done := make(chan error, 1)

	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				err := mcpServer.Run(serverContext, &mcp.StdioTransport{})
				done <- err

				options := make([]fx.ShutdownOption, 0, 1)
				if err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("MCP server stopped: %v", err)
					options = append(options, fx.ExitCode(1))
				}
				if err := shutdowner.Shutdown(options...); err != nil {
					log.Printf("application shutdown failed: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			cancelServer()
			select {
			case err := <-done:
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
}
