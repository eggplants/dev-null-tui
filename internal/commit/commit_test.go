package commit

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// fakeServer plays /dev/null's CLI endpoints: it picks the key it was told is on GitHub, and records what was
// committed.
type fakeServer struct {
	t      *testing.T
	github string // keyID of the key on the user's GitHub account

	mu        sync.Mutex
	lookup    string
	payload   []byte
	signature string
	images    [][]byte
	names     []string
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch r.URL.Path {
	case "/api/cli/key-lookups":
		body, _ := io.ReadAll(r.Body)
		f.lookup = string(body)
		if !strings.Contains(f.lookup, f.github) {
			http.Error(w, "None of your SSH keys are on the GitHub account of a /dev/null user.", http.StatusNotFound)
			return
		}
		fmt.Fprintln(w, f.github+" comment")
	case "/api/cli/commits":
		f.signature = r.Header.Get("X-SSH-Signature")
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				f.t.Error(err)
			}
			file, _, _ := r.FormFile("payload")
			f.payload, _ = io.ReadAll(file)
			for _, h := range r.MultipartForm.File["images[]"] {
				img, _ := h.Open()
				data, _ := io.ReadAll(img)
				f.images = append(f.images, data)
				f.names = append(f.names, h.Filename)
			}
		} else {
			f.payload, _ = io.ReadAll(r.Body)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, "Committed: https://dev-null.sh/@me/commits/1\x1b[31m\n")
	default:
		http.NotFound(w, r)
	}
}

func writeKey(t *testing.T, dir, name string, passphrase []byte) ssh.Signer {
	t.Helper()

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}

	var block *pem.Block
	if passphrase != nil {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(key, "", passphrase)
	} else {
		block, err = ssh.MarshalPrivateKey(key, "")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".pub"), ssh.MarshalAuthorizedKey(signer.PublicKey()), 0o644); err != nil {
		t.Fatal(err)
	}

	return signer
}

// verify checks the commit's signature with `ssh-keygen -Y verify`, as the server does.
func verify(t *testing.T, key ssh.PublicKey, ns string, payload []byte, signature string) {
	t.Helper()

	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not found")
	}

	blob, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	sig, allowed := filepath.Join(dir, "sig"), filepath.Join(dir, "allowed")
	if err := os.WriteFile(sig, pem.EncodeToMemory(&pem.Block{Type: "SSH SIGNATURE", Bytes: blob}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allowed, []byte("me "+string(ssh.MarshalAuthorizedKey(key))), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("ssh-keygen", "-Y", "verify", "-f", allowed, "-I", "me", "-n", ns, "-s", sig)
	cmd.Stdin = strings.NewReader(string(payload))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("signature doesn't verify under %s: %v\n%s", ns, err, out)
	}
}

func setup(t *testing.T) (dir string, srv *httptest.Server, fake *fakeServer, opts Options) {
	t.Helper()

	dir = t.TempDir()
	fake = &fakeServer{t: t}
	srv = httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	opts = Options{
		BaseURL:     strings.Replace(srv.URL, "http://127.0.0.1", "http://localhost", 1),
		SSHDir:      dir,
		AgentSocket: filepath.Join(dir, "no-agent"),
		HTTPClient:  srv.Client(),
	}

	return dir, srv, fake, opts
}

func TestCommitsWithTheKeyOnGitHub(t *testing.T) {
	dir, _, fake, opts := setup(t)
	writeKey(t, dir, "id_other", nil)
	mine := writeKey(t, dir, "id_ed25519", nil)
	fake.github = keyID(mine.PublicKey())

	opts.ReplyTo = "0be0130"
	opts.Message = "Agreed!"
	answer, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}

	if answer != "Committed: https://dev-null.sh/@me/commits/1[31m\n" {
		t.Errorf("answer = %q, want it without the escape character", answer)
	}
	if strings.Count(fake.lookup, "\n") != 2 {
		t.Errorf("looked up %q, want both keys", fake.lookup)
	}

	lines := strings.Split(string(fake.payload), "\n")
	if len(lines) != 4 || len(lines[1]) != 32 || lines[2] != "0be0130" || lines[3] != "Agreed!" {
		t.Errorf("payload = %q", fake.payload)
	}
	verify(t, mine.PublicKey(), "devnull-commit", fake.payload, fake.signature)
}

func TestSignsTheImages(t *testing.T) {
	dir, _, fake, opts := setup(t)
	mine := writeKey(t, dir, "id_ed25519", nil)
	fake.github = keyID(mine.PublicKey())

	png := filepath.Join(t.TempDir(), "shipped.png")
	if err := os.WriteFile(png, []byte("\x89PNG fake"), 0o600); err != nil {
		t.Fatal(err)
	}

	opts.Images = []string{png, png}
	opts.Message = "Shipped!"
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256([]byte("\x89PNG fake"))
	hash := hex.EncodeToString(sum[:])
	lines := strings.Split(string(fake.payload), "\n")
	if len(lines) != 5 || lines[2] != "" || lines[3] != hash+","+hash || lines[4] != "Shipped!" {
		t.Errorf("payload = %q", fake.payload)
	}
	if len(fake.images) != 2 || string(fake.images[0]) != "\x89PNG fake" || fake.names[0] != "shipped.png" {
		t.Errorf("images = %q, names = %q", fake.images, fake.names)
	}
	verify(t, mine.PublicKey(), "devnull-commit-images", fake.payload, fake.signature)
}

func TestSignsThroughTheAgent(t *testing.T) {
	dir, _, fake, opts := setup(t)

	_, key, _ := ed25519.GenerateKey(rand.Reader)
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: key}); err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(key)
	fake.github = keyID(signer.PublicKey())

	socket := filepath.Join(dir, "agent.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go agent.ServeAgent(keyring, conn)
		}
	}()

	opts.AgentSocket = socket
	opts.Message = "from the agent"
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	verify(t, signer.PublicKey(), "devnull-commit", fake.payload, fake.signature)
}

func TestAsksForThePassphrase(t *testing.T) {
	dir, _, fake, opts := setup(t)
	mine := writeKey(t, dir, "id_ed25519", []byte("secret"))
	fake.github = keyID(mine.PublicKey())

	var asked string
	opts.Passphrase = func(path string) ([]byte, error) {
		asked = path
		return []byte("secret"), nil
	}
	opts.Message = "locked key"
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	if asked != filepath.Join(dir, "id_ed25519") {
		t.Errorf("asked for the passphrase of %q", asked)
	}
	verify(t, mine.PublicKey(), "devnull-commit", fake.payload, fake.signature)
}

func TestRefusesAKeyItDidNotOffer(t *testing.T) {
	dir, _, _, opts := setup(t)
	writeKey(t, dir, "id_ed25519", nil)

	// The server answers with a key that isn't ours: the commit must never be signed or sent.
	other := keyID(writeKey(t, t.TempDir(), "elsewhere", nil).PublicKey())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/commits" {
			t.Error("the commit was sent")
		}
		fmt.Fprintln(w, other)
	}))
	defer srv.Close()
	opts.BaseURL, opts.HTTPClient = srv.URL, srv.Client()

	opts.Message = "hi there"
	if _, err := Run(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "unexpected answer") {
		t.Errorf("err = %v", err)
	}
}

func TestPassesOnTheServersMessage(t *testing.T) {
	dir, _, fake, opts := setup(t)
	writeKey(t, dir, "id_ed25519", nil)
	fake.github = "ssh-ed25519 AAAAnotmine"

	opts.Message = "hi there"
	_, err := Run(context.Background(), opts)
	if err == nil || err.Error() != "None of your SSH keys are on the GitHub account of a /dev/null user." {
		t.Errorf("err = %v", err)
	}
}

func TestChecksInputsBeforeSendingAnything(t *testing.T) {
	big := filepath.Join(t.TempDir(), "big.png")
	if err := os.WriteFile(big, make([]byte, MaxImageSize+1), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		opts Options
		err  string
	}{
		{Options{BaseURL: "http://example.com", Message: "hi there"}, "not an https:// URL"},
		{Options{BaseURL: "https://dev-null.sh", Message: "   "}, "empty commit message"},
		{Options{BaseURL: "https://dev-null.sh", Message: "hi there", ReplyTo: "abc; rm"}, "--reply"},
		{Options{BaseURL: "https://dev-null.sh", Message: "hi there", Images: []string{"a", "b", "c", "d"}}, "up to 3 images"},
		{Options{BaseURL: "https://dev-null.sh", Message: "hi there", Images: []string{big}}, "larger than 10 MB"},
		{Options{BaseURL: "https://dev-null.sh", Message: "hi there", Images: []string{"/nonexistent.png"}}, "could not read the image"},
	} {
		_, err := Run(context.Background(), tt.opts)
		if err == nil || !strings.Contains(err.Error(), tt.err) {
			t.Errorf("Run(%+v) = %v, want %q", tt.opts, err, tt.err)
		}
	}
}
