// Package commit commits to /dev/null signed with an SSH key on the user's GitHub account, as the site's
// `curl -fsSL https://dev-null.sh/commit | bash -s -- 'text'` script does. Unlike the TUI, it needs no device
// approval: /dev/null checks the signature against the keys GitHub publishes for the user.
package commit

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/eggplants/dev-null-tui/internal/sshsig"
)

const (
	namespace       = "devnull-commit"
	imagesNamespace = "devnull-commit-images"

	MaxImages    = 3
	MaxImageSize = 10240000
)

var replyPattern = regexp.MustCompile(`^[A-Za-z0-9:/._@-]*$`)

type Options struct {
	BaseURL string
	// ReplyTo is the hash of the commit to reply to, or its link.
	ReplyTo string
	Images  []string
	Message string

	// Passphrase asks for the passphrase of an encrypted private key. Without it such keys can only sign through
	// the SSH agent.
	Passphrase func(path string) ([]byte, error)
	// SSHDir and AgentSocket default to ~/.ssh and $SSH_AUTH_SOCK.
	SSHDir      string
	AgentSocket string
	HTTPClient  *http.Client
}

type image struct {
	name string
	data []byte
}

// Run makes the commit and returns what /dev/null answered, such as the commit's link.
func Run(ctx context.Context, opts Options) (string, error) {
	if err := checkBaseURL(opts.BaseURL); err != nil {
		return "", err
	}
	if !replyPattern.MatchString(opts.ReplyTo) {
		return "", errors.New("--reply takes the hash of a commit or its link")
	}
	if strings.TrimSpace(opts.Message) == "" {
		return "", errors.New("aborting commit due to empty commit message")
	}
	if len(opts.Images) > MaxImages {
		return "", fmt.Errorf("a commit can carry up to %d images", MaxImages)
	}

	// Read each image once, so what is hashed and signed is what is sent.
	images := make([]image, 0, len(opts.Images))
	for _, path := range opts.Images {
		data, err := readImage(path)
		if err != nil {
			return "", err
		}
		images = append(images, image{name: filepath.Base(path), data: data})
	}

	if opts.SSHDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		opts.SSHDir = filepath.Join(home, ".ssh")
	}
	if opts.AgentSocket == "" {
		opts.AgentSocket = os.Getenv("SSH_AUTH_SOCK")
	}
	client := opts.HTTPClient
	if client == nil {
		client = newHTTPClient()
	}
	base := strings.TrimRight(opts.BaseURL, "/")

	agentClient, closeAgent := connectAgent(opts.AgentSocket)
	defer closeAgent()

	// 1. Find a local key that is on the GitHub account of a /dev/null user.
	cands := candidates(agentClient, opts.SSHDir)
	if len(cands) == 0 {
		return "", fmt.Errorf("no Ed25519 or RSA SSH key found in your SSH agent or in %s", opts.SSHDir)
	}

	var lookup strings.Builder
	for _, c := range cands {
		lookup.WriteString(keyID(c.key) + "\n")
	}
	answer, err := send(ctx, client, base+"/api/cli/key-lookups", "text/plain; charset=utf-8", strings.NewReader(lookup.String()), nil)
	if err != nil {
		return "", err
	}

	// Only ever sign with one of our own keys, whatever /dev/null answers.
	chosen := match(cands, answer)
	if chosen == nil {
		return "", fmt.Errorf("unexpected answer from %s", base)
	}

	// 2. Sign with that key.
	signer, err := chosen.signer(agentClient, opts.Passphrase)
	if err != nil {
		return "", err
	}

	payload, ns, err := buildPayload(opts.ReplyTo, opts.Message, images)
	if err != nil {
		return "", err
	}
	signature, err := sshsig.SignBase64(signer, ns, payload)
	if err != nil {
		return "", fmt.Errorf("could not sign your commit: %w", err)
	}
	headers := map[string]string{"X-SSH-Signature": signature}

	// 3. Commit: the raw payload, or the payload as a file next to the images.
	if len(images) == 0 {
		return send(ctx, client, base+"/api/cli/commits", "text/plain; charset=utf-8", bytes.NewReader(payload), headers)
	}

	body, contentType, err := multipartBody(payload, images)
	if err != nil {
		return "", err
	}

	return send(ctx, client, base+"/api/cli/commits", contentType, body, headers)
}

// checkBaseURL refuses to send a commit anywhere but https, or http on this machine, as the script does.
func checkBaseURL(base string) error {
	u, err := url.Parse(base)
	if err != nil {
		return err
	}

	switch {
	case u.Scheme == "https" && u.Host != "":
		return nil
	case u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"):
		return nil
	default:
		return fmt.Errorf("refusing to send your commit to %s: it is not an https:// URL", base)
	}
}

func readImage(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("could not read the image %s", path)
	}
	if info.Size() > MaxImageSize {
		return nil, fmt.Errorf("the image %s is larger than 10 MB", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read the image %s", path)
	}
	if len(data) > MaxImageSize {
		return nil, fmt.Errorf("the image %s is larger than 10 MB", path)
	}

	return data, nil
}

func match(cands []*candidate, answer string) *candidate {
	line, _, _ := strings.Cut(answer, "\n")
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return nil
	}

	id := fields[0] + " " + fields[1]
	for _, c := range cands {
		if keyID(c.key) == id {
			return c
		}
	}

	return nil
}

// buildPayload makes "<timestamp>\n<nonce>\n<reply to>\n<body>", with the comma-separated SHA-256 of each image on
// its own line before the body when there are images, and says which namespace signs it.
func buildPayload(replyTo, message string, images []image) ([]byte, string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", err
	}

	lines := []string{strconv.FormatInt(time.Now().Unix(), 10), hex.EncodeToString(nonce), replyTo}
	ns := namespace
	if len(images) > 0 {
		hashes := make([]string, len(images))
		for i, img := range images {
			sum := sha256.Sum256(img.data)
			hashes[i] = hex.EncodeToString(sum[:])
		}
		lines = append(lines, strings.Join(hashes, ","))
		ns = imagesNamespace
	}

	return []byte(strings.Join(append(lines, message), "\n")), ns, nil
}

func multipartBody(payload []byte, images []image) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("payload", "commit")
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(payload); err != nil {
		return nil, "", err
	}

	for _, img := range images {
		part, err := w.CreateFormFile("images[]", img.name)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(img.data); err != nil {
			return nil, "", err
		}
	}

	if err := w.Close(); err != nil {
		return nil, "", err
	}

	return &buf, w.FormDataContentType(), nil
}

// newHTTPClient never follows redirects and only speaks TLS 1.2 or later, as the script's curl does.
func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}

	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// send posts body and returns the plain-text answer, or an error carrying it unless the request succeeded.
func send(ctx context.Context, client *http.Client, target, contentType string, body io.Reader, headers map[string]string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach %s: %w", req.URL.Host, err)
	}
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	text := sanitize(string(data))

	if res.StatusCode < 200 || res.StatusCode > 299 {
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("unexpected answer from %s (HTTP %d)", req.URL.Host, res.StatusCode)
		}
		return "", errors.New(strings.TrimSpace(text))
	}

	return text, nil
}

// sanitize removes control characters, such as terminal escape sequences, keeping tabs and newlines.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r >= 0x20 && r != 0x7f {
			return r
		}
		return -1
	}, s)
}

// Prompt reads a line from the terminal, for a commit message not given on the command line.
func Prompt(tty *os.File, label string) (string, error) {
	fmt.Fprint(tty, label)
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}

	return strings.TrimRight(line, "\r\n"), nil
}
