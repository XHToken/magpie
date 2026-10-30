// Package testkeyvault supplies synthetic, process-independent key protection
// for integration fixtures. Only test files import it. It is not an OS vault.
package testkeyvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

type Protector struct{}

func fixtureAEAD() cipher.AEAD {
	key := sha256.Sum256([]byte("synthetic integration fixture; never production keys"))
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	return aead
}

func (Protector) ProtectResultKey(raw []byte) ([]byte, error) {
	aead := fixtureAEAD()
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, raw, nil)...), nil
}
func (Protector) UnprotectResultKey(raw []byte) ([]byte, error) {
	aead := fixtureAEAD()
	if len(raw) < aead.NonceSize() {
		return nil, errors.New("invalid synthetic key envelope")
	}
	return aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], nil)
}
