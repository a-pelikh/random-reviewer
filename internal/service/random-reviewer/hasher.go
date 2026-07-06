package random_reviewer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

type hasher[T ~string] struct {
	gcm cipher.AEAD
	key []byte
}

func newHasher[T ~string](salt string) (*hasher[T], error) {
	key := sha256.Sum256([]byte(salt))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("could not create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("could not create gcm: %w", err)
	}
	return &hasher[T]{gcm: gcm, key: key[:]}, nil
}

func (h *hasher[T]) Encode(plaintext T) (T, error) {
	mac := hmac.New(sha256.New, h.key)
	mac.Write([]byte(plaintext))
	nonce := mac.Sum(nil)[:h.gcm.NonceSize()]

	ciphertext := h.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return T(base64.RawURLEncoding.EncodeToString(ciphertext)), nil
}

func (h *hasher[T]) Decode(encoded T) (T, error) {
	data, err := base64.RawURLEncoding.DecodeString(string(encoded))
	if err != nil {
		return "", fmt.Errorf("could not decode base64: %w", err)
	}
	nonceSize := h.gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := h.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("could not decrypt: %w", err)
	}
	return T(plaintext), nil
}
