// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package app

import (
	"context"
	"errors"
	"log"

	"github.com/kordax/beget-api-mcp-server/internal/transport"
	"go.uber.org/fx"
)

func RegisterLifecycle(lifecycle fx.Lifecycle, shutdowner fx.Shutdowner, runtime *transport.Runtime) {
	serverContext, cancelServer := context.WithCancel(context.Background())
	done := make(chan error, 1)

	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			if err := runtime.Prepare(); err != nil {
				cancelServer()
				return err
			}
			if endpoint := runtime.Endpoint(); endpoint != "" {
				log.Printf("MCP %s transport listening on %s", runtime.Mode(), endpoint)
			}
			go func() {
				err := runtime.Run(serverContext)
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
			shutdownErr := runtime.Shutdown(ctx)
			select {
			case err := <-done:
				if errors.Is(err, context.Canceled) {
					err = nil
				}
				return errors.Join(shutdownErr, err)
			case <-ctx.Done():
				return errors.Join(shutdownErr, ctx.Err())
			}
		},
	})
}
