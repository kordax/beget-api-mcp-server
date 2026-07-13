// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package app

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunValidatesCredentialCommands(t *testing.T) {
	var output bytes.Buffer
	assert.Equal(t, 1, Run([]string{"credentials"}, &output))
	assert.Contains(t, output.String(), "requires set, check, or delete")

	output.Reset()
	assert.Equal(t, 1, Run([]string{"credentials", "unknown"}, &output))
	assert.Contains(t, output.String(), "unknown credentials command")
}
