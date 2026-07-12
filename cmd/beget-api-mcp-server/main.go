// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/kordax/beget-api-mcp-server/internal/app"
	"go.uber.org/fx"
)

func main() {
	fx.New(app.Module).Run()
}
