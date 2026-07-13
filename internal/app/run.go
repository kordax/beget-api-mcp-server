// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package app

import (
	"context"
	"fmt"
	"io"

	"github.com/kordax/beget-api-mcp-server/internal/credentials"
	"github.com/kordax/beget-api-mcp-server/internal/updater"
	"go.uber.org/dig"
	"go.uber.org/fx"
)

func Run(arguments []string, errorOutput io.Writer) int {
	if credentials.IsCommand(arguments) {
		var command *credentials.Command
		application := fx.New(
			credentials.Module,
			fx.Provide(credentials.NewCommand),
			fx.Populate(&command),
			fx.NopLogger,
		)
		if err := application.Err(); err != nil {
			_, _ = fmt.Fprintf(errorOutput, "initialize credentials command: %v\n", err)
			return 1
		}
		if err := command.Run(arguments[1:]); err != nil {
			_, _ = fmt.Fprintf(errorOutput, "%v\n", err)
			return 1
		}
		return 0
	}
	if updater.IsCommand(arguments) {
		var command *updater.Command
		application := fx.New(updater.Module, fx.Populate(&command), fx.NopLogger)
		if err := application.Err(); err != nil {
			_, _ = fmt.Fprintf(errorOutput, "initialize upgrade command: %v\n", err)
			return 1
		}
		if err := command.Run(context.Background(), arguments[1:]); err != nil {
			_, _ = fmt.Fprintf(errorOutput, "upgrade: %v\n", err)
			return 1
		}
		return 0
	}

	return runServer(errorOutput, Module)
}

func runServer(errorOutput io.Writer, options ...fx.Option) int {
	options = append(options, fx.NopLogger)
	application := fx.New(options...)
	if err := application.Err(); err != nil {
		_, _ = fmt.Fprintf(errorOutput, "start MCP server: %v\n", dig.RootCause(err))
		return 1
	}

	application.Run()
	return 0
}
