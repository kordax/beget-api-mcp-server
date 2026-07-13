// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package passwordpolicy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateMailboxAcceptsConfirmedPolicy(t *testing.T) {
	for _, password := range []string{
		"Abc123!",
		"Aa1!aa",
		"A1!" + strings.Repeat("a", MailboxMaximumLength-3),
		"Aa1" + MailboxAllowedSymbols,
	} {
		assert.Empty(t, ValidateMailbox(password))
	}
}

func TestValidateMailboxReportsSafeViolations(t *testing.T) {
	for name, testCase := range map[string]struct {
		password string
		codes    []ViolationCode
	}{
		"too short":              {password: "Aa1!", codes: []ViolationCode{ViolationLength}},
		"too long":               {password: "Aa1!" + strings.Repeat("a", MailboxMaximumLength), codes: []ViolationCode{ViolationLength}},
		"missing letter":         {password: "123456!", codes: []ViolationCode{ViolationMissingLetter}},
		"missing digit":          {password: "abcdef!", codes: []ViolationCode{ViolationMissingDigit}},
		"missing symbol":         {password: "abcdef1", codes: []ViolationCode{ViolationMissingSymbol}},
		"space is unsupported":   {password: "Abc1! x", codes: []ViolationCode{ViolationUnsupportedCharacter}},
		"unicode is unsupported": {password: "Пароль1!", codes: []ViolationCode{ViolationMissingLetter, ViolationUnsupportedCharacter}},
		"backslash unsupported":  {password: `Abc1!\x`, codes: []ViolationCode{ViolationUnsupportedCharacter}},
	} {
		t.Run(name, func(t *testing.T) {
			violations := ValidateMailbox(testCase.password)
			actual := make([]ViolationCode, len(violations))
			for index, violation := range violations {
				actual[index] = violation.Code
				assert.NotContains(t, violation.Message, testCase.password)
			}
			assert.ElementsMatch(t, testCase.codes, actual)
		})
	}
}

func TestMailboxSchemaPatternMatchesPolicy(t *testing.T) {
	pattern := MailboxAllowedCharacterPattern()
	require.NotEmpty(t, pattern)
	assert.Contains(t, pattern, `\-`)
	assert.Contains(t, pattern, `\]`)
	assert.Equal(t, "", ValidationMessage("Abc123!"))
	assert.NotContains(t, ValidationMessage("secret value"), "secret value")
}
