// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package passwordpolicy

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MailboxMinimumLength  = 6
	MailboxMaximumLength  = 64
	MailboxAllowedSymbols = `.,/<>?;:"'` + "`" + `!@#$%^&*()[]{}_+-=|~`
)

type ViolationCode string

const (
	ViolationLength               ViolationCode = "length"
	ViolationMissingLetter        ViolationCode = "missing_letter"
	ViolationMissingDigit         ViolationCode = "missing_digit"
	ViolationMissingSymbol        ViolationCode = "missing_symbol"
	ViolationUnsupportedCharacter ViolationCode = "unsupported_character"
)

type Violation struct {
	Code    ViolationCode
	Message string
}

func MailboxRequirement() string {
	return fmt.Sprintf(
		"mailbox_password must contain %d to %d characters using only English letters, digits, and these symbols: %s; include at least one letter, one digit, and one symbol",
		MailboxMinimumLength, MailboxMaximumLength, MailboxAllowedSymbols,
	)
}

func ValidateMailbox(password string) []Violation {
	violations := make([]Violation, 0, 4)
	length := utf8.RuneCountInString(password)
	if length < MailboxMinimumLength || length > MailboxMaximumLength {
		violations = append(violations, Violation{
			Code:    ViolationLength,
			Message: fmt.Sprintf("mailbox_password must contain %d to %d characters", MailboxMinimumLength, MailboxMaximumLength),
		})
	}

	hasLetter, hasDigit, hasSymbol, hasUnsupported := false, false, false, false
	for _, character := range password {
		switch {
		case character >= 'A' && character <= 'Z', character >= 'a' && character <= 'z':
			hasLetter = true
		case character >= '0' && character <= '9':
			hasDigit = true
		case strings.ContainsRune(MailboxAllowedSymbols, character):
			hasSymbol = true
		default:
			hasUnsupported = true
		}
	}
	if !hasLetter {
		violations = append(violations, Violation{Code: ViolationMissingLetter, Message: "mailbox_password must contain at least one English letter"})
	}
	if !hasDigit {
		violations = append(violations, Violation{Code: ViolationMissingDigit, Message: "mailbox_password must contain at least one digit"})
	}
	if !hasSymbol {
		violations = append(violations, Violation{
			Code:    ViolationMissingSymbol,
			Message: "mailbox_password must contain at least one allowed symbol from: " + MailboxAllowedSymbols,
		})
	}
	if hasUnsupported {
		violations = append(violations, Violation{
			Code:    ViolationUnsupportedCharacter,
			Message: "mailbox_password contains unsupported characters; allowed characters are English letters, digits, and: " + MailboxAllowedSymbols,
		})
	}
	return violations
}

func ValidationMessage(password string) string {
	violations := ValidateMailbox(password)
	if len(violations) == 0 {
		return ""
	}
	messages := make([]string, len(violations))
	for index, violation := range violations {
		messages[index] = violation.Message
	}
	return strings.Join(messages, "; ")
}

func MailboxAllowedCharacterPattern() string {
	return `^[A-Za-z0-9` + escapeCharacterClass(MailboxAllowedSymbols) + `]+$`
}

func escapeCharacterClass(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `-`, `\-`, `[`, `\[`, `]`, `\]`, `^`, `\^`)
	return replacer.Replace(value)
}
