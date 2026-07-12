// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package app

import (
	"github.com/kordax/beget-api-mcp-server/internal/beget"
	"github.com/kordax/beget-api-mcp-server/internal/config"
	"github.com/kordax/beget-api-mcp-server/internal/server"
	"go.uber.org/fx"
)

var Module = fx.Module("app",
	config.Module,
	beget.Module,
	server.Module,
	fx.Invoke(RegisterLifecycle),
)
