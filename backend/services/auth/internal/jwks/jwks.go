package jwks

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
)

type Signer struct {
	kid       string
	private   *rsa.PrivateKey
	publicJWK JWK
}

type JWKS struct {
	Keys []JWK `json:"keys"`
}

type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func NewSigner(kid string, privateKeyPEM string) (*Signer, error) {
	var priv *rsa.PrivateKey
	var err error

	if privateKeyPEM != "" {
		priv, err = parseRSAPrivateKeyPEM([]byte(privateKeyPEM))
		if err != nil {
			return nil, err
		}
	} else {
		priv, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("generate rsa key: %w", err)
		}
	}

	pubJWK, err := rsaPublicJWK(kid, &priv.PublicKey)
	if err != nil {
		return nil, err
	}

	return &Signer{
		kid:       kid,
		private:   priv,
		publicJWK: pubJWK,
	}, nil
}

func (s *Signer) PrivateKey() *rsa.PrivateKey { return s.private }

func (s *Signer) JWKS() JWKS {
	return JWKS{Keys: []JWK{s.publicJWK}}
}

func parseRSAPrivateKeyPEM(b []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("invalid private key pem")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	return key, nil
}

func rsaPublicJWK(kid string, pub *rsa.PublicKey) (JWK, error) {
	if pub == nil || pub.N == nil {
		return JWK{}, fmt.Errorf("nil public key")
	}

	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())

	return JWK{
		Kty: "RSA",
		Kid: kid,
		Use: "sig",
		Alg: "RS256",
		N:   n,
		E:   e,
	}, nil
}
