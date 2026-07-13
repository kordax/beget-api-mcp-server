// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package app

import (
	"fmt"
	"io"

	"github.com/kordax/beget-api-mcp-server/internal/credentials"
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

	fx.New(Module).Run()
	return 0
}
