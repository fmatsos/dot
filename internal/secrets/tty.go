package secrets

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// IsTerminal reports whether r is a terminal (an *os.File on a tty).
func IsTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// PromptHidden asks for a line on /dev/tty without echo, whatever stdin and stdout are.
// An interrupt restores the terminal and cancels the prompt.
func PromptHidden(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer tty.Close()
	fd := int(tty.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return "", err
	}
	fmt.Fprint(tty, prompt)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	type reply struct {
		b   []byte
		err error
	}
	ch := make(chan reply, 1)
	go func() { b, err := term.ReadPassword(fd); ch <- reply{b, err} }()
	select {
	case r := <-ch:
		fmt.Fprintln(tty)
		return string(r.b), r.err
	case <-sig:
		_ = term.Restore(fd, state)
		fmt.Fprintln(tty)
		return "", errors.New("interrompu")
	}
}
