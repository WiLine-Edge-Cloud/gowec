package ui

import (
	"fmt"
	"io"
	"sync"
	"time"
)

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner shows that something is under way. It draws only on a terminal;
// elsewhere it stays silent and the caller's final line says what happened.
type Spinner struct {
	w      io.Writer
	active bool
	mu     sync.Mutex
	text   string
	stop   chan struct{}
	done   chan struct{}
}

// StartSpinner starts a spinner on w (normally stderr) when tty is true.
func StartSpinner(w io.Writer, tty bool, text string) *Spinner {
	s := &Spinner{w: w, active: tty, text: text, stop: make(chan struct{}), done: make(chan struct{})}
	if !tty {
		close(s.done)
		return s
	}
	go func() {
		defer close(s.done)
		tick := time.NewTicker(90 * time.Millisecond)
		defer tick.Stop()
		for i := 0; ; i++ {
			s.mu.Lock()
			fmt.Fprintf(s.w, "\r\033[K%s %s", Accent.Render(frames[i%len(frames)]), s.text)
			s.mu.Unlock()
			select {
			case <-s.stop:
				fmt.Fprint(s.w, "\r\033[K")
				return
			case <-tick.C:
			}
		}
	}()
	return s
}

// Set changes the spinner's text.
func (s *Spinner) Set(text string) {
	s.mu.Lock()
	s.text = text
	s.mu.Unlock()
}

// Stop clears the spinner's line.
func (s *Spinner) Stop() {
	if s.active {
		select {
		case <-s.stop:
		default:
			close(s.stop)
		}
	}
	<-s.done
}
