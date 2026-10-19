package sshsig

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestVerifiesWithSshKeygen checks the signature against `ssh-keygen -Y verify`, which is what the server mirrors.
func TestVerifiesWithSshKeygen(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not found")
	}

	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	for name, key := range map[string]any{"ed25519": edKey, "rsa": rsaKey} {
		t.Run(name, func(t *testing.T) {
			signer, err := ssh.NewSignerFromKey(key)
			if err != nil {
				t.Fatal(err)
			}

			message := []byte("1760534400\n3f9c2a7e5b1d4c8f9a0b2c3d4e5f6a7b\nPOST\n/api/v1/commits\n{\"body\":\"hi\"}")
			blob, err := Sign(signer, "devnull-api", message)
			if err != nil {
				t.Fatal(err)
			}

			dir := t.TempDir()
			sigPath := filepath.Join(dir, "msg.sig")
			allowedPath := filepath.Join(dir, "allowed_signers")
			armored := pem.EncodeToMemory(&pem.Block{Type: "SSH SIGNATURE", Bytes: blob})
			allowed := "tui " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + "\n"
			if err := os.WriteFile(sigPath, armored, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(allowedPath, []byte(allowed), 0o600); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command("ssh-keygen", "-Y", "verify", "-f", allowedPath, "-I", "tui", "-n", "devnull-api", "-s", sigPath)
			cmd.Stdin = strings.NewReader(string(message))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("ssh-keygen -Y verify: %v\n%s", err, out)
			}
		})
	}
}
