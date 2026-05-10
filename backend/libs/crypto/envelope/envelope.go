package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

type KMS interface {
	KeyID() string
	WrapDEK(dek []byte) ([]byte, error)
	UnwrapDEK(wrapped []byte) ([]byte, error)
}

type LocalKMS struct {
	keyID     string
	masterKey []byte
}

func NewLocalKMS(keyID string, masterKeyBase64 string) (*LocalKMS, error) {
	key, err := base64.StdEncoding.DecodeString(masterKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode master key base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes (got %d)", len(key))
	}
	return &LocalKMS{keyID: keyID, masterKey: key}, nil
}

func (k *LocalKMS) KeyID() string { return k.keyID }

func (k *LocalKMS) WrapDEK(dek []byte) ([]byte, error) {
	return aesGCMEncrypt(k.masterKey, dek)
}

func (k *LocalKMS) UnwrapDEK(wrapped []byte) ([]byte, error) {
	return aesGCMDecrypt(k.masterKey, wrapped)
}

func Encrypt(kms KMS, plaintext []byte) (keyID string, wrappedDEK []byte, ciphertext []byte, err error) {
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return "", nil, nil, fmt.Errorf("generate data key: %w", err)
	}

	ciphertext, err = aesGCMEncrypt(dek, plaintext)
	if err != nil {
		return "", nil, nil, err
	}

	wrappedDEK, err = kms.WrapDEK(dek)
	if err != nil {
		return "", nil, nil, err
	}

	return kms.KeyID(), wrappedDEK, ciphertext, nil
}

func Decrypt(kms KMS, wrappedDEK []byte, ciphertext []byte) ([]byte, error) {
	dek, err := kms.UnwrapDEK(wrappedDEK)
	if err != nil {
		return nil, err
	}
	return aesGCMDecrypt(dek, ciphertext)
}

func aesGCMEncrypt(key []byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("read nonce: %w", err)
	}

	encrypted := aead.Seal(nil, nonce, plaintext, nil)
	return append(nonce, encrypted...), nil
}

func aesGCMDecrypt(key []byte, in []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}

	if len(in) < aead.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce := in[:aead.NonceSize()]
	ciphertext := in[aead.NonceSize():]

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}
