// Package api talks to the /dev/null API, signing each request with an SSH key the user approved as a device.
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eggplants/dev-null-tui/internal/sshsig"
	"golang.org/x/crypto/ssh"
)

const signatureNamespace = "devnull-api"

// Error is an error answer from the API. Code is empty for errors the API doesn't tell apart.
type Error struct {
	Status     int
	Message    string `json:"message"`
	Code       string `json:"code"`
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("%d: %s", e.Status, e.Message)
	}

	return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message)
}

type Client struct {
	BaseURL    string
	Signer     ssh.Signer
	HTTPClient *http.Client
}

// maxResponseSize is the most of an answer the client reads, far more than any page of the API.
const maxResponseSize = 4 << 20

func New(baseURL string, signer ssh.Signer) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Signer:  signer,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			// A redirect would send the signed request somewhere it wasn't signed for.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Do sends a request to path (with its query string), signed when the client has a signer, and decodes the
// JSON answer into out unless out is nil.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Signer != nil {
		if err := c.sign(req, body); err != nil {
			return err
		}
	}

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseSize+1))
	if err != nil {
		return err
	}
	if len(data) > maxResponseSize {
		return fmt.Errorf("the answer to %s %s is larger than %d MB", method, path, maxResponseSize>>20)
	}

	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return &Error{Status: res.StatusCode, Message: "the server redirected to " + res.Header.Get("Location")}
	}
	if res.StatusCode >= 400 {
		apiErr := &Error{Status: res.StatusCode}
		if json.Unmarshal(data, apiErr) != nil || apiErr.Message == "" {
			apiErr.Message = http.StatusText(res.StatusCode)
		}
		if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil {
			apiErr.RetryAfter = time.Duration(seconds) * time.Second
		}

		return apiErr
	}

	if out == nil || len(data) == 0 {
		return nil
	}

	if err := json.Unmarshal(data, out); err != nil {
		return err
	}
	sanitize(out)

	return nil
}

// sign signs "<timestamp>\n<nonce>\n<METHOD>\n<path and query>\n<body>", as the server's SignedRequests reads it.
func (c *Client) sign(req *http.Request, body []byte) error {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonceHex := hex.EncodeToString(nonce)
	message := strings.Join([]string{timestamp, nonceHex, req.Method, req.URL.RequestURI(), string(body)}, "\n")

	signature, err := sshsig.SignBase64(c.Signer, signatureNamespace, []byte(message))
	if err != nil {
		return err
	}

	req.Header.Set("X-Devnull-Timestamp", timestamp)
	req.Header.Set("X-Devnull-Nonce", nonceHex)
	req.Header.Set("X-SSH-Signature", signature)

	return nil
}
