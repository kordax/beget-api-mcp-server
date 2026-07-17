// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package credentials

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

type Validator interface {
	Validate(context.Context, Credentials) error
}

type Command struct {
	store       Store
	validator   Validator
	input       *os.File
	output      io.Writer
	errorOutput io.Writer
}

func NewCommand(store Store, validator Validator) *Command {
	return &Command{store: store, validator: validator, input: os.Stdin, output: os.Stdout, errorOutput: os.Stderr}
}

func IsCommand(arguments []string) bool {
	return len(arguments) > 0 && arguments[0] == "credentials"
}

func (command *Command) Run(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("credentials command requires set, check, or delete")
	}
	switch arguments[0] {
	case "set":
		return command.set(ctx, arguments[1:])
	case "check":
		if len(arguments) != 1 {
			return errors.New("credentials check does not accept arguments")
		}
		value, err := command.store.Load()
		if err != nil {
			return err
		}
		if err := command.validator.Validate(ctx, value); err != nil {
			return err
		}
		_, err = fmt.Fprintln(command.output, "Beget credentials are valid and authorized")
		return err
	case "delete":
		if len(arguments) != 1 {
			return errors.New("credentials delete does not accept arguments")
		}
		if err := command.store.Delete(); err != nil {
			return err
		}
		_, err := fmt.Fprintln(command.output, "Beget credentials were removed from the persistent credential store")
		return err
	default:
		return fmt.Errorf("unknown credentials command %q", arguments[0])
	}
}

func (command *Command) set(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("credentials set", flag.ContinueOnError)
	flags.SetOutput(command.errorOutput)
	login := flags.String("login", "", "Beget hosting account login")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("credentials set accepts only --login")
	}
	if strings.TrimSpace(*login) == "" {
		return errors.New("credentials set requires --login")
	}

	apiKey, err := readAPIKey(command.input, command.errorOutput)
	if err != nil {
		return err
	}
	value := Credentials{Login: strings.TrimSpace(*login), APIKey: apiKey}
	if err := command.validator.Validate(ctx, value); err != nil {
		return err
	}
	if err := command.store.Save(value); err != nil {
		return err
	}
	_, err = fmt.Fprintln(command.output, "Beget credentials were validated and saved in the persistent credential store")
	return err
}

func readAPIKey(input *os.File, prompt io.Writer) (string, error) {
	if input == nil {
		return "", errors.New("API key input is unavailable")
	}
	var value string
	if term.IsTerminal(int(input.Fd())) {
		if _, err := fmt.Fprint(prompt, "Beget API key: "); err != nil {
			return "", err
		}
		secret, err := term.ReadPassword(int(input.Fd()))
		_, _ = fmt.Fprintln(prompt)
		if err != nil {
			return "", fmt.Errorf("read Beget API key: %w", err)
		}
		value = string(secret)
	} else {
		line, err := bufio.NewReader(input).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read Beget API key from stdin: %w", err)
		}
		value = strings.TrimRight(line, "\r\n")
	}
	if value == "" {
		return "", errors.New("beget API key is required")
	}
	return value, nil
}
