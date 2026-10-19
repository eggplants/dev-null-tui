package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func newRequest(t *testing.T) *Request {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	r, err := NewRequest("https://dev-null.sh/", key, "devnull-tui on test")
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func TestAuthorizeURL(t *testing.T) {
	r := newRequest(t)

	u, err := url.Parse(r.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "dev-null.sh" || u.Path != "/mobile/authorize" {
		t.Errorf("URL = %s", r.AuthorizeURL)
	}

	q := u.Query()
	challenge := sha256.Sum256([]byte(r.CodeVerifier))
	for name, want := range map[string]string{
		"code_challenge":        base64.RawURLEncoding.EncodeToString(challenge[:]),
		"code_challenge_method": "S256",
		"redirect_uri":          "devnull://auth/callback",
		"state":                 r.State,
		"device_name":           "devnull-tui on test",
	} {
		if got := q.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	// The server takes a 43-character fingerprint and a 43 to 128 character verifier.
	if fp := q.Get("fingerprint"); !strings.HasPrefix(fp, "SHA256:") || len(fp) != len("SHA256:")+43 {
		t.Errorf("fingerprint = %q", fp)
	}
	if n := len(r.CodeVerifier); n < 43 || n > 128 {
		t.Errorf("verifier is %d characters", n)
	}
}

func TestCode(t *testing.T) {
	r := newRequest(t)

	for _, tt := range []struct {
		callback string
		code     string
		err      string
	}{
		{"devnull://auth/callback?code=abc&state=" + r.State, "abc", ""},
		{"devnull://auth/callback?code=abc&state=other", "", "state"},
		{"devnull://auth/callback?error=access_denied&state=" + r.State, "", "cancelled"},
		{"devnull://auth/callback?state=" + r.State, "", "no code"},
		{"https://evil.example/?code=abc&state=" + r.State, "", "not a devnull"},
	} {
		code, err := r.Code(tt.callback)
		if code != tt.code || (tt.err == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tt.err) {
			t.Errorf("Code(%q) = %q, %v; want %q, %q", tt.callback, code, err, tt.code, tt.err)
		}
	}
}

func TestForwardReachesListen(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	urls, err := Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		if err := Forward("devnull://auth/callback?code=x"); err != nil {
			t.Error(err)
		}
	}()

	select {
	case got := <-urls:
		if got != "devnull://auth/callback?code=x" {
			t.Errorf("got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no URL came through")
	}
}

func TestDesktopQuote(t *testing.T) {
	if got := desktopQuote(`/home/me/my bin/$x"y`); got != `"/home/me/my bin/\$x\"y"` {
		t.Errorf("desktopQuote = %s", got)
	}
}
