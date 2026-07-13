// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package updater

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSpinnerStaysSilentOutsideTerminal(t *testing.T) {
	var output bytes.Buffer
	progress := &spinner{output: &output}
	expected := errors.New("operation failed")

	err := progress.Run("Working...", func() error { return expected })

	assert.ErrorIs(t, err, expected)
	assert.Empty(t, output.String())
}

func TestSpinnerAnimatesAndClearsTerminalLine(t *testing.T) {
	var output bytes.Buffer
	progress := &spinner{output: &output, enabled: true, interval: time.Millisecond}

	assert.NoError(t, progress.Run("Working...", func() error {
		time.Sleep(4 * time.Millisecond)
		return nil
	}))
	assert.Contains(t, output.String(), "⠋ Working...")
	assert.Contains(t, output.String(), "⠙ Working...")
	assert.Contains(t, output.String(), "\r\x1b[2K")
}
