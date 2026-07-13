// Copyright (c) 2026 Dmitry Morozov (kordax) <kordaxmint@gmail.com>
// SPDX-License-Identifier: MIT

package updater

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

const spinnerInterval = 80 * time.Millisecond

var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type spinner struct {
	output   io.Writer
	enabled  bool
	interval time.Duration
}

func newSpinner(output io.Writer) *spinner {
	file, isFile := output.(*os.File)
	return &spinner{
		output:   output,
		enabled:  isFile && term.IsTerminal(int(file.Fd())),
		interval: spinnerInterval,
	}
}

func (spinner *spinner) Run(label string, operation func() error) error {
	if !spinner.enabled {
		return operation()
	}

	done := make(chan struct{})
	animationDone := make(chan struct{})
	go spinner.animate(label, done, animationDone)

	err := operation()
	close(done)
	<-animationDone
	return err
}

func (spinner *spinner) animate(label string, done <-chan struct{}, animationDone chan<- struct{}) {
	defer close(animationDone)
	interval := spinner.interval
	if interval <= 0 {
		interval = spinnerInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	frame := 0
	spinner.draw(spinnerFrames[frame], label)
	for {
		select {
		case <-done:
			_, _ = fmt.Fprint(spinner.output, "\r\x1b[2K")
			return
		case <-ticker.C:
			frame = (frame + 1) % len(spinnerFrames)
			spinner.draw(spinnerFrames[frame], label)
		}
	}
}

func (spinner *spinner) draw(frame, label string) {
	_, _ = fmt.Fprintf(spinner.output, "\r%s %s", frame, label)
}
