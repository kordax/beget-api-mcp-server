// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package updater

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"go.uber.org/fx"
)

type Command struct {
	updater *Updater
	output  io.Writer
}

var Module = fx.Module("updater", fx.Provide(New, NewCommand))

func NewCommand(updater *Updater) *Command {
	return &Command{updater: updater, output: os.Stdout}
}

func IsCommand(arguments []string) bool {
	return len(arguments) > 0 && arguments[0] == "upgrade"
}

func (command *Command) Run(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	check := flags.Bool("check", false, "check for an update without installing it")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("upgrade accepts at most one version")
	}
	requestedVersion := ""
	if flags.NArg() == 1 {
		requestedVersion = flags.Arg(0)
	}
	if *check {
		if requestedVersion != "" {
			return errors.New("upgrade --check does not accept a version")
		}
		latest, err := command.updater.LatestVersion(ctx)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(command.output, "Current version: v%s\nLatest version: %s\n", command.updater.currentVersion, latest)
		return err
	}

	version, err := command.updater.Upgrade(ctx, requestedVersion)
	if err != nil {
		return err
	}
	if version == "v"+command.updater.currentVersion {
		_, err = fmt.Fprintf(command.output, "beget-api-mcp-server %s is already installed\n", version)
		return err
	}
	_, err = fmt.Fprintf(command.output, "Updated beget-api-mcp-server to %s. Restart MCP clients to use it.\n", version)
	return err
}
