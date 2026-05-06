package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// tokenHeader is the JWT header portion.
type tokenHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// tokenPayload is the JWT payload portion.
type tokenPayload struct {
	UserID    string   `json:"sub"`
	Email     string   `json:"email"`
	Roles     []string `json:"roles"`
	Issuer    string   `json:"iss"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
}

func encodeToken(secret, issuer string, claims Claims) (string, error) {
	header := tokenHeader{Alg: "HS256", Typ: "JWT"}
	payload := tokenPayload{
		UserID:    claims.UserID,
		Email:     claims.Email,
		Roles:     claims.Roles,
		Issuer:    issuer,
		ExpiresAt: claims.ExpiresAt.Unix(),
		IssuedAt:  claims.IssuedAt.Unix(),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("marshal header: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signingInput := headerB64 + "." + payloadB64
	sig := signHMAC(secret, signingInput)

	return signingInput + "." + sig, nil
}

func decodeToken(secret, issuer string, tokenString string) (Claims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("invalid token format")
	}

	// Verify signature
	signingInput := parts[0] + "." + parts[1]
	expectedSig := signHMAC(secret, signingInput)
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return Claims{}, fmt.Errorf("invalid signature")
	}

	// Decode header
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("decode header: %w", err)
	}
	var header tokenHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return Claims{}, fmt.Errorf("unmarshal header: %w", err)
	}
	if header.Alg != "HS256" || header.Typ != "JWT" {
		return Claims{}, fmt.Errorf("unexpected token type")
	}

	// Decode payload
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("decode payload: %w", err)
	}
	var payload tokenPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return Claims{}, fmt.Errorf("unmarshal payload: %w", err)
	}

	if payload.Issuer != issuer {
		return Claims{}, fmt.Errorf("invalid issuer")
	}

	return Claims{
		UserID:    payload.UserID,
		Email:     payload.Email,
		Roles:     payload.Roles,
		ExpiresAt: time.Unix(payload.ExpiresAt, 0),
		IssuedAt:  time.Unix(payload.IssuedAt, 0),
	}, nil
}

func signHMAC(secret, data string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
