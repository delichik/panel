package sshx

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/pem"
	"math/big"

	"golang.org/x/crypto/ssh"
)

// KeySummary carries non-secret metadata derived from a private key so the
// UI can show a fingerprint/name without ever exposing key material.
type KeySummary struct {
	Algorithm   string `json:"algorithm,omitempty"`
	Bits        int    `json:"bits,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Comment     string `json:"comment,omitempty"`
}

// SummarizePrivateKey parses a private key and returns its non-secret
// summary. It never returns the key material itself. The second return
// value is false when the key cannot be parsed.
func SummarizePrivateKey(privateKey []byte, passphrase string) (KeySummary, bool) {
	var signer ssh.Signer
	var err error
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(privateKey, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(privateKey)
	}
	if err != nil {
		return KeySummary{}, false
	}
	pub := signer.PublicKey()
	return KeySummary{
		Algorithm:   keyAlgorithmName(pub.Type()),
		Bits:        publicKeyBits(pub),
		Fingerprint: ssh.FingerprintSHA256(pub),
		Comment:     opensshPrivateKeyComment(privateKey),
	}, true
}

func keyAlgorithmName(keyType string) string {
	switch keyType {
	case ssh.KeyAlgoED25519:
		return "ED25519"
	case ssh.KeyAlgoRSA:
		return "RSA"
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		return "ECDSA"
	case ssh.KeyAlgoDSA:
		return "DSA"
	case ssh.KeyAlgoSKED25519:
		return "ED25519-SK"
	case ssh.KeyAlgoSKECDSA256:
		return "ECDSA-SK"
	default:
		return keyType
	}
}

func publicKeyBits(pub ssh.PublicKey) int {
	cpk, ok := pub.(ssh.CryptoPublicKey)
	if !ok {
		return 0
	}
	switch k := cpk.CryptoPublicKey().(type) {
	case *rsa.PublicKey:
		return k.N.BitLen()
	case ed25519.PublicKey:
		return 256
	case *ecdsa.PublicKey:
		return k.Curve.Params().BitSize
	default:
		return 0
	}
}

// opensshPrivateKeyComment reads the trailing comment (the key name) from an
// unencrypted OpenSSH private key. Encrypted private sections are not exposed
// by x/crypto, so they return an empty comment instead of leaking material.
func opensshPrivateKeyComment(privateKey []byte) string {
	block, _ := pem.Decode(privateKey)
	if block == nil || block.Type != "OPENSSH PRIVATE KEY" {
		return ""
	}
	const magic = "openssh-key-v1\x00"
	body := block.Bytes
	if !bytes.HasPrefix(body, []byte(magic)) {
		return ""
	}
	var outer struct {
		CipherName   string
		KdfName      string
		KdfOpts      string
		NumKeys      uint32
		PubKey       []byte
		PrivKeyBlock []byte
		Rest         []byte `ssh:"rest"`
	}
	if err := ssh.Unmarshal(body[len(magic):], &outer); err != nil || outer.NumKeys != 1 {
		return ""
	}
	if outer.CipherName != "none" {
		return ""
	}
	var header struct {
		Check1  uint32
		Check2  uint32
		Keytype string
		Rest    []byte `ssh:"rest"`
	}
	if err := ssh.Unmarshal(outer.PrivKeyBlock, &header); err != nil || header.Check1 != header.Check2 {
		return ""
	}
	switch header.Keytype {
	case ssh.KeyAlgoED25519:
		var k struct {
			Pub     []byte
			Priv    []byte
			Comment string
			Pad     []byte `ssh:"rest"`
		}
		if err := ssh.Unmarshal(header.Rest, &k); err != nil {
			return ""
		}
		return k.Comment
	case ssh.KeyAlgoRSA:
		var k struct {
			N       *big.Int
			E       *big.Int
			D       *big.Int
			Iqmp    *big.Int
			P       *big.Int
			Q       *big.Int
			Comment string
			Pad     []byte `ssh:"rest"`
		}
		if err := ssh.Unmarshal(header.Rest, &k); err != nil {
			return ""
		}
		return k.Comment
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		var k struct {
			Curve   string
			Pub     []byte
			D       *big.Int
			Comment string
			Pad     []byte `ssh:"rest"`
		}
		if err := ssh.Unmarshal(header.Rest, &k); err != nil {
			return ""
		}
		return k.Comment
	default:
		return ""
	}
}
