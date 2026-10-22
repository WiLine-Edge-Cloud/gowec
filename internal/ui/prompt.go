package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ErrNotInteractive is returned when a question needs a terminal and there is none.
var ErrNotInteractive = errors.New("no terminal to ask on")

// Prompter asks the person at the keyboard. The shell answers through its
// line editor, which owns stdin while it runs; one-shot commands use the terminal.
type Prompter interface {
	Line(prompt string) (string, error)
	Secret(prompt string) (string, error)
}

// TermPrompter reads from stdin, writing questions to stderr.
type TermPrompter struct{}

func (TermPrompter) Line(prompt string) (string, error) {
	if !IsTerminal(os.Stdin) {
		return "", ErrNotInteractive
	}
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (TermPrompter) Secret(prompt string) (string, error) {
	if !IsTerminal(os.Stdin) {
		return "", ErrNotInteractive
	}
	fmt.Fprint(os.Stderr, prompt)
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(string(data)), err
}

// Confirm asks a yes/no question that defaults to no.
func Confirm(p Prompter, question string) (bool, error) {
	answer, err := p.Line(Yellow.Render("?") + " " + question + " " + Dim.Render("[y/N]") + " ")
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes", nil
}

// IsTerminal reports whether f is a terminal.
func IsTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }
