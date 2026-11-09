package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignsRequestsWithFiles(t *testing.T) {
	requireSshKeygen(t)
	signer, dir := newSigner(t), t.TempDir()

	images := []File{{Name: "a.png", Data: []byte("first image")}, {Name: "b.webp", Data: []byte("second image")}}
	var got NewCommit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not multipart: %v", err)
			return
		}
		if len(r.MultipartForm.Value) != 0 {
			t.Errorf("sent fields beside the payload: %v", r.MultipartForm.Value)
		}

		f, _, err := r.FormFile("payload")
		if err != nil {
			t.Errorf("no payload: %v", err)
			return
		}
		payload, _ := io.ReadAll(f)
		_ = json.Unmarshal(payload, &got)

		var hashes []string
		for i, fh := range r.MultipartForm.File["images[]"] {
			f, _ := fh.Open()
			data, _ := io.ReadAll(f)
			if fh.Filename != images[i].Name || string(data) != string(images[i].Data) {
				t.Errorf("image %d is %s %q", i, fh.Filename, data)
			}
			sum := sha256.Sum256(data)
			hashes = append(hashes, hex.EncodeToString(sum[:]))
		}

		message := strings.Join([]string{
			r.Header.Get("X-Devnull-Timestamp"), r.Header.Get("X-Devnull-Nonce"), r.Method, r.URL.RequestURI(),
			strings.Join(hashes, ","), string(payload),
		}, "\n")
		if err := verifyMessage(dir, signer, r.Header.Get("X-SSH-Signature"), "devnull-api-images", message); err != nil {
			t.Error(err)
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"c1","short_hash":"abc1234","body":"shipped"}}`))
	}))
	defer srv.Close()

	created, err := New(srv.URL, signer).CreateCommitWithImages(context.Background(),
		NewCommit{Body: "shipped", QuotedCommitID: "q1", ImageDescriptions: []string{"a cat", ""}}, images)
	if err != nil {
		t.Fatal(err)
	}
	if created.ShortHash != "abc1234" {
		t.Errorf("answered %+v", created)
	}
	if got.Body != "shipped" || got.QuotedCommitID != "q1" || len(got.ImageDescriptions) != 2 || got.ImageDescriptions[0] != "a cat" {
		t.Errorf("sent %+v", got)
	}
}
