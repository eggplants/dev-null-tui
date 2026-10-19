package commit

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// candidate is a public key that may be on the user's GitHub account, and where its private half is.
type candidate struct {
	key      ssh.PublicKey
	inAgent  bool
	privPath string
}

// keyID is "<type> <base64>", the form the key lookup takes and answers with, without the comment.
func keyID(key ssh.PublicKey) string {
	return key.Type() + " " + base64.StdEncoding.EncodeToString(key.Marshal())
}

func supported(key ssh.PublicKey) bool {
	return key.Type() == ssh.KeyAlgoED25519 || key.Type() == ssh.KeyAlgoRSA
}

// candidates lists the Ed25519 and RSA keys in the SSH agent and in sshDir (those whose private key is next to
// the .pub file), agent first, each once.
func candidates(agentClient agent.ExtendedAgent, sshDir string) []*candidate {
	var list []*candidate
	seen := map[string]*candidate{}

	if agentClient != nil {
		keys, _ := agentClient.List()
		for _, k := range keys {
			key, err := ssh.ParsePublicKey(k.Blob)
			if err != nil || !supported(key) || seen[keyID(key)] != nil {
				continue
			}
			c := &candidate{key: key, inAgent: true}
			seen[keyID(key)] = c
			list = append(list, c)
		}
	}

	pubs, _ := filepath.Glob(filepath.Join(sshDir, "*.pub"))
	for _, pub := range pubs {
		priv := strings.TrimSuffix(pub, ".pub")
		if info, err := os.Stat(priv); err != nil || !info.Mode().IsRegular() {
			continue
		}

		key, err := firstPublicKey(pub)
		if err != nil || !supported(key) {
			continue
		}
		if c := seen[keyID(key)]; c != nil {
			if c.privPath == "" {
				c.privPath = priv
			}
			continue
		}

		c := &candidate{key: key, privPath: priv}
		seen[keyID(key)] = c
		list = append(list, c)
	}

	return list
}

func firstPublicKey(path string) (ssh.PublicKey, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && line == "" {
		return nil, err
	}

	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))

	return key, err
}

// connectAgent connects to the SSH agent at socket, or returns nil when there is none.
func connectAgent(socket string) (agent.ExtendedAgent, func()) {
	if socket == "" {
		return nil, func() {}
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, func() {}
	}

	return agent.NewClient(conn), func() { conn.Close() }
}

// signer returns a signer for the candidate: the agent's if it holds the key, otherwise one from the private key
// file, asking for its passphrase if it has one.
func (c *candidate) signer(agentClient agent.ExtendedAgent, passphrase func(path string) ([]byte, error)) (ssh.Signer, error) {
	if c.inAgent && agentClient != nil {
		signers, err := agentClient.Signers()
		if err != nil {
			return nil, err
		}
		for _, s := range signers {
			if keyID(s.PublicKey()) == keyID(c.key) {
				return s, nil
			}
		}
	}

	if c.privPath == "" {
		return nil, fmt.Errorf("could not find the private key of %s", keyID(c.key))
	}

	data, err := os.ReadFile(c.privPath)
	if err != nil {
		return nil, err
	}

	s, err := ssh.ParsePrivateKey(data)
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		if passphrase == nil {
			return nil, fmt.Errorf("%s needs a passphrase: add it to your SSH agent with ssh-add", c.privPath)
		}
		pass, perr := passphrase(c.privPath)
		if perr != nil {
			return nil, perr
		}
		s, err = ssh.ParsePrivateKeyWithPassphrase(data, pass)
	}
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", c.privPath, err)
	}

	if keyID(s.PublicKey()) != keyID(c.key) {
		return nil, fmt.Errorf("%s doesn't match %s.pub", c.privPath, c.privPath)
	}

	return s, nil
}
