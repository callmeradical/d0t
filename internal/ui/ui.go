// Package ui provides the user-facing printer used by d0t commands.
// Output is intentionally plain (no colors, no spinners) so it composes
// cleanly with logs and CI environments.
package ui

import (
	"fmt"
	"io"
)

// Printer is the minimal output sink used by primitives. The two-method
// surface keeps it trivial to mock in tests.
type Printer interface {
	// Info prints a normal message (ends with newline).
	Info(format string, args ...any)
	// Verbose prints a message only when verbose mode is enabled.
	Verbose(format string, args ...any)
}

// NewPrinter returns a Printer that writes to w. If verbose is false,
// Verbose calls are silently dropped.
func NewPrinter(w io.Writer, verbose bool) Printer {
	return &printer{w: w, verbose: verbose}
}

type printer struct {
	w       io.Writer
	verbose bool
}

func (p *printer) Info(format string, args ...any) {
	fmt.Fprintf(p.w, format, args...)
	fmt.Fprintln(p.w)
}

func (p *printer) Verbose(format string, args ...any) {
	if !p.verbose {
		return
	}
	fmt.Fprintf(p.w, format, args...)
	fmt.Fprintln(p.w)
}
