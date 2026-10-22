//go:build unix

package tui

import (
	"bytes"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
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

	if _, err := f.WriteString(query); err != nil {
		return nil
	}

	// macOS can't poll /dev/tty with kqueue, so the file takes no deadline; select waits for the answers instead.
	deadline := time.Now().Add(500 * time.Millisecond)
	var answer []byte
	buf := make([]byte, 256)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return answer
		}
		var fds unix.FdSet
		fds.Set(fd)
		timeout := unix.NsecToTimeval(left.Nanoseconds())
		n, err := unix.Select(fd+1, &fds, nil, nil, &timeout)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n == 0 {
			return answer
		}

		n, err = unix.Read(fd, buf)
		if n > 0 {
			answer = append(answer, buf[:n]...)
		}
		if i := bytes.Index(answer, []byte("\x1b[?")); i >= 0 && bytes.IndexByte(answer[i:], 'c') >= 0 {
			return answer
		}
		if err != nil && !errors.Is(err, unix.EINTR) && !errors.Is(err, unix.EAGAIN) {
			return answer
		}
	}
}
