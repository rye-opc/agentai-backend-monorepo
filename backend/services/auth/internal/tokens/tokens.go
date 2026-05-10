package tokens

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Issuer struct {
	KID      string
	Issuer   string
	Audience string

	AccessTTL time.Duration
	Private   *rsa.PrivateKey
}

func (i *Issuer) NewAccessToken(userID string, extraClaims map[string]any) (token string, expiresInSeconds int32, err error) {
	now := time.Now()
	exp := now.Add(i.AccessTTL)

	claims := jwt.MapClaims{
		"iss": i.Issuer,
		"sub": userID,
		"aud": i.Audience,
		"iat": now.Unix(),
		"exp": exp.Unix(),
		"typ": "access",
	}
	for k, v := range extraClaims {
		claims[k] = v
	}

	t := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	t.Header["kid"] = i.KID

	signed, err := t.SignedString(i.Private)
	if err != nil {
		return "", 0, fmt.Errorf("sign jwt: %w", err)
	}

	return signed, int32(time.Until(exp).Seconds()), nil
}

func NewRefreshToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	return raw, sum[:], nil
}

func HashRefreshToken(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty refresh token")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("invalid refresh token")
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}
