package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

func jwtSecret() string {
	if v := strings.TrimSpace(os.Getenv("JWT_SECRET")); v != "" {
		return v
	}
	return "textile-erp-dev-secret"
}

func SignJWT(claims map[string]interface{}) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	if _, ok := claims["iat"]; !ok {
		claims["iat"] = time.Now().Unix()
	}
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(12 * time.Hour).Unix()
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	head := base64.RawURLEncoding.EncodeToString(headerBytes)
	body := base64.RawURLEncoding.EncodeToString(payloadBytes)
	unsigned := head + "." + body
	sig := signJWT(unsigned)
	return unsigned + "." + sig, nil
}

func VerifyJWT(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token structure")
	}
	unsigned := parts[0] + "." + parts[1]
	expected := signJWT(unsigned)
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, errors.New("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	if exp, ok := claims["exp"].(float64); ok {
		if int64(exp) < time.Now().Unix() {
			return nil, errors.New("token expired")
		}
	}
	return claims, nil
}

func signJWT(unsigned string) string {
	mac := hmac.New(sha256.New, []byte(jwtSecret()))
	_, _ = mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
