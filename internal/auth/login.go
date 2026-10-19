// Package auth signs the TUI in the way the mobile app does: the user approves its key on the web's authorize page,
// which hands a one-time code back through the devnull:// URL scheme, and the TUI redeems it with its PKCE verifier.
package auth

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	CallbackURL = "devnull://auth/callback"
	scheme      = "devnull"
)

// Request is one sign-in attempt: the authorize page's parameters and the verifier that redeems its code.
type Request struct {
	AuthorizeURL string
	State        string
	CodeVerifier string
}

func NewRequest(baseURL string, key ssh.PublicKey, deviceName string) (*Request, error) {
	verifier, err := randomBytes(32)
	if err != nil {
		return nil, err
	}
	state, err := randomBytes(16)
	if err != nil {
		return nil, err
	}

	r := &Request{
		State:        hex.EncodeToString(state),
		CodeVerifier: base64.RawURLEncoding.EncodeToString(verifier),
	}
	challenge := sha256.Sum256([]byte(r.CodeVerifier))

	query := url.Values{
		"fingerprint":           {ssh.FingerprintSHA256(key)},
		"device_name":           {deviceName},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		"state":                 {r.State},
		"redirect_uri":          {CallbackURL},
	}
	r.AuthorizeURL = strings.TrimRight(baseURL, "/") + "/mobile/authorize?" + query.Encode()

	return r, nil
}

// Code reads the code out of the URL the authorize page redirected to, checking it answers this request.
func (r *Request) Code(callback string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(callback))
	if err != nil {
		return "", err
	}
	if u.Scheme != scheme {
		return "", fmt.Errorf("not a %s:// URL: %s", scheme, callback)
	}

	q := u.Query()
	if q.Get("state") != r.State {
		return "", errors.New("the callback's state doesn't match this sign-in")
	}
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			return "", errors.New("sign-in was cancelled on the web")
		}

		return "", fmt.Errorf("sign-in failed: %s", e)
	}
	if q.Get("code") == "" {
		return "", errors.New("the callback has no code")
	}

	return q.Get("code"), nil
}

// SocketPath is where a running login waits for the devnull:// URL that `dn handle-url` forwards.
func SocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}

	return filepath.Join(dir, fmt.Sprintf("devnull-tui-%d.sock", os.Getuid()))
}

// Listen waits on the socket for callback URLs, sending each to the returned channel until ctx is done.
func Listen(ctx context.Context) (<-chan string, error) {
	path := SocketPath()
	_ = os.Remove(path)

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}

	urls := make(chan string)
	go func() {
		<-ctx.Done()
		ln.Close()
		_ = os.Remove(path)
	}()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(io.LimitReader(conn, 4096)).ReadString('\n')
			conn.Close()

			select {
			case urls <- strings.TrimSpace(line):
			case <-ctx.Done():
				return
			}
		}
	}()

	return urls, nil
}

// Forward hands a callback URL to the login waiting on the socket.
func Forward(callback string) error {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return fmt.Errorf("no `dn login` is waiting for %s: %w", callback, err)
	}
	defer conn.Close()

	_, err = fmt.Fprintln(conn, callback)

	return err
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)

	return b, err
}
