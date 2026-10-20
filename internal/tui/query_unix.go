//go:build unix

package tui

import (
	"bytes"
	"os"
	"syscall"
	"time"

	"golang.org/x/term"
)

// queryTerminal writes escape sequences to the terminal and reads its answers until the one to the primary device
// attributes, ESC [ ? ... c, comes in, giving up after a moment.
func queryTerminal(query string) []byte {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	defer f.Close()

	fd := int(f.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil
	}
	defer term.Restore(fd, state)
	// Fd made the file blocking; reads need it non-blocking for the deadline to work.
	_ = syscall.SetNonblock(fd, true)

	if _, err := f.WriteString(query); err != nil {
		return nil
	}
	if err := f.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		return nil
	}

	var answer []byte
	buf := make([]byte, 256)
	for {
		n, err := f.Read(buf)
		answer = append(answer, buf[:n]...)
		if i := bytes.Index(answer, []byte("\x1b[?")); i >= 0 && bytes.IndexByte(answer[i:], 'c') >= 0 {
			return answer
		}
		if err != nil {
			return answer
		}
	}
}
