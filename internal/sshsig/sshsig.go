// Package sshsig signs messages in the SSHSIG format that `ssh-keygen -Y sign` produces.
// See https://github.com/openssh/openssh-portable/blob/master/PROTOCOL.sshsig
package sshsig

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/ssh"
)

const (
	magic         = "SSHSIG"
	version       = 1
	hashAlgorithm = "sha512"
)

// Sign returns the binary SSHSIG signature of message under namespace.
func Sign(signer ssh.Signer, namespace string, message []byte) ([]byte, error) {
	digest := sha512.Sum512(message)

	signedData := []byte(magic)
	signedData = append(signedData, ssh.Marshal(struct {
		Namespace     string
		Reserved      string
		HashAlgorithm string
		Hash          string
	}{namespace, "", hashAlgorithm, string(digest[:])})...)

	sig, err := sign(signer, signedData)
	if err != nil {
		return nil, err
	}

	blob := []byte(magic)
	blob = append(blob, ssh.Marshal(struct {
		Version       uint32
		PublicKey     string
		Namespace     string
		Reserved      string
		HashAlgorithm string
		Signature     string
	}{version, string(signer.PublicKey().Marshal()), namespace, "", hashAlgorithm, string(ssh.Marshal(sig))})...)

	return blob, nil
}

// SignBase64 returns the signature as the body of the armored file `ssh-keygen -Y sign` writes, on one line.
func SignBase64(signer ssh.Signer, namespace string, message []byte) (string, error) {
	blob, err := Sign(signer, namespace, message)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(blob), nil
}

// sign uses rsa-sha2-512 for RSA keys, as ssh-keygen does, since ssh-rsa (SHA-1) signatures are refused.
func sign(signer ssh.Signer, data []byte) (*ssh.Signature, error) {
	if signer.PublicKey().Type() != ssh.KeyAlgoRSA {
		return signer.Sign(rand.Reader, data)
	}

	algorithmSigner, ok := signer.(ssh.AlgorithmSigner)
	if !ok {
		return nil, fmt.Errorf("sshsig: RSA signer can't sign with %s", ssh.KeyAlgoRSASHA512)
	}

	return algorithmSigner.SignWithAlgorithm(rand.Reader, data, ssh.KeyAlgoRSASHA512)
}
