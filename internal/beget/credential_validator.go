// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package beget

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kordax/beget-api-mcp-server/internal/config"
	"github.com/kordax/beget-api-mcp-server/internal/credentials"
	"go.uber.org/fx"
)

var CredentialValidationModule = fx.Module("beget-credential-validation",
	fx.Provide(
		NewHTTPClient,
		NewCredentialValidator,
	),
)

type credentialValidator struct {
	baseURL    string
	httpClient *http.Client
}

func NewCredentialValidator(configuration config.Config, httpClient *http.Client) credentials.Validator {
	return &credentialValidator{baseURL: configuration.BaseURL, httpClient: httpClient}
}

func (validator *credentialValidator) Validate(ctx context.Context, value credentials.Credentials) error {
	client, err := NewClient(validator.baseURL, value.Login, value.APIKey, validator.httpClient)
	if err != nil {
		return fmt.Errorf("prepare Beget authorization check: %w", err)
	}
	if _, err := client.Call(ctx, "user", "getAccountInfo", nil); err != nil {
		return err
	}
	return nil
}
