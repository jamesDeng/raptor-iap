package modelproviders

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func ReadEncryptionKey(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("invalid credential key path")
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != 32 {
		return nil, errors.New("invalid credential key file")
	}
	key, e := os.ReadFile(path)
	if e != nil || len(key) != 32 {
		return nil, errors.New("invalid credential key file")
	}
	return key, nil
}

type Ciphertext struct {
	Data       []byte
	Nonce      []byte
	KeyVersion string
}

func EncryptCredential(plaintext, key []byte, keyVersion string, providerID ProviderID) (Ciphertext, error) {
	if len(key) != 32 || keyVersion == "" || providerID == "" || len(plaintext) == 0 || len(plaintext) > 65536 {
		return Ciphertext{}, errors.New("invalid credential encryption input")
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return Ciphertext{}, e
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return Ciphertext{}, e
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return Ciphertext{}, e
	}
	aad := []byte(string(providerID) + "|" + keyVersion)
	return Ciphertext{Data: gcm.Seal(nil, nonce, plaintext, aad), Nonce: nonce, KeyVersion: keyVersion}, nil
}

func DecryptCredential(value Ciphertext, key []byte, providerID ProviderID) ([]byte, error) {
	if len(key) != 32 || value.KeyVersion == "" || providerID == "" {
		return nil, errors.New("invalid credential decryption input")
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	if len(value.Nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid credential nonce")
	}
	plain, e := gcm.Open(nil, value.Nonce, value.Data, []byte(string(providerID)+"|"+value.KeyVersion))
	if e != nil {
		return nil, errors.New("credential decrypt failed")
	}
	return plain, nil
}
