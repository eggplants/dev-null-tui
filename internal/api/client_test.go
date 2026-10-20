package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}

	return signer
}

func requireSshKeygen(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not found")
	}
}

// verify checks a request's signature with `ssh-keygen -Y verify`, the way the server checks it. It runs in the
// server's goroutine, so it returns its failure instead of failing the test.
func verify(dir string, signer ssh.Signer, r *http.Request, body []byte) error {
	blob, err := base64.StdEncoding.DecodeString(r.Header.Get("X-SSH-Signature"))
	if err != nil {
		return fmt.Errorf("X-SSH-Signature isn't base64: %w", err)
	}

	message := strings.Join([]string{
		r.Header.Get("X-Devnull-Timestamp"), r.Header.Get("X-Devnull-Nonce"), r.Method, r.URL.RequestURI(), string(body),
	}, "\n")

	sig := filepath.Join(dir, "sig")
	allowed := filepath.Join(dir, "allowed")
	if err := os.WriteFile(sig, pem.EncodeToMemory(&pem.Block{Type: "SSH SIGNATURE", Bytes: blob}), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(allowed, []byte("tui "+string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), 0o600); err != nil {
		return err
	}

	cmd := exec.Command("ssh-keygen", "-Y", "verify", "-f", allowed, "-I", "tui", "-n", "devnull-api", "-s", sig)
	cmd.Stdin = strings.NewReader(message)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("signature doesn't verify: %w\n%s", err, out)
	}

	return nil
}

func TestSignsRequests(t *testing.T) {
	requireSshKeygen(t)
	signer, dir := newSigner(t), t.TempDir()

	var checked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := verify(dir, signer, r, body); err != nil {
			t.Error(err)
		}

		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if len(r.Header.Get("X-Devnull-Nonce")) != 32 {
			t.Errorf("nonce %q isn't 32 hex characters", r.Header.Get("X-Devnull-Nonce"))
		}
		checked = true

		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"data":{"id":"c1","short_hash":"abc1234","body":"hi there","status":"pending"}}`)
	}))
	defer srv.Close()

	commit, err := New(srv.URL, signer).CreateCommit(context.Background(), NewCommit{Body: "hi there", ParentID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("the server never saw the request")
	}
	if commit.ShortHash != "abc1234" || commit.Status != "pending" {
		t.Errorf("commit = %+v", commit)
	}
}

func TestSignsTheQueryString(t *testing.T) {
	requireSshKeygen(t)
	signer, dir := newSigner(t), t.TempDir()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := verify(dir, signer, r, nil); err != nil {
			t.Error(err)
		}
		if r.URL.RequestURI() != "/api/v1/log/latest?cursor=abc%3D" {
			t.Errorf("RequestURI = %q", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"c1"}],"is_ranked":false,"suggestions":[],"meta":{"next_cursor":"next"}}`)
	}))
	defer srv.Close()

	page, err := New(srv.URL, signer).Log(context.Background(), LogLatest, "abc=")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 || page.NextCursor != "next" {
		t.Errorf("page = %+v", page)
	}
}

func TestDecodesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"message":"Too Many Attempts.","code":"rate_limited"}`)
	}))
	defer srv.Close()

	_, err := New(srv.URL, newSigner(t)).Me(context.Background())

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *Error", err)
	}
	if apiErr.Status != 429 || apiErr.Code != "rate_limited" || apiErr.RetryAfter != 12*time.Second {
		t.Errorf("err = %+v", apiErr)
	}
}

func TestPageReadsEitherCursor(t *testing.T) {
	var fromMeta, fromReplyPreview Page[Commit]
	if err := fromMeta.UnmarshalJSON([]byte(`{"data":[],"meta":{"next_cursor":"a"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := fromReplyPreview.UnmarshalJSON([]byte(`{"data":[],"next_cursor":"b"}`)); err != nil {
		t.Fatal(err)
	}

	if fromMeta.NextCursor != "a" || fromReplyPreview.NextCursor != "b" {
		t.Errorf("cursors = %q, %q", fromMeta.NextCursor, fromReplyPreview.NextCursor)
	}
}

func TestNewCommitsCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/api/v1/log/following?since=c1" {
			t.Errorf("RequestURI = %q", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"data":[],"is_ranked":false,"new_commits_count":3,"suggestions":[],"meta":{"next_cursor":null}}`)
	}))
	defer srv.Close()

	n, err := New(srv.URL, newSigner(t)).NewCommitsCount(context.Background(), LogFollowing, "c1")
	if err != nil || n != 3 {
		t.Errorf("NewCommitsCount = %d, %v, want 3", n, err)
	}
}
