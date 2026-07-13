// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/kordax/beget-api-mcp-server/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stderr))
}
