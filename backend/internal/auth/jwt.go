// jwt.go —— HMAC-SHA256 JWT 签发 / 校验（最小实现，无第三方依赖）
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// signHS256 按 RFC 7519 简化实现，仅支持 alg=HS256 / typ=JWT。
func signHS256(secret []byte, claims map[string]any) (string, error) {
	hdr, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	pl, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	h := enc.EncodeToString(hdr) + "." + enc.EncodeToString(pl)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(h))
	sig := enc.EncodeToString(mac.Sum(nil))
	return h + "." + sig, nil
}

// verifyHS256 校验签名并返回 payload map。
func verifyHS256(secret []byte, token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("jwt: malformed token")
	}
	enc := base64.RawURLEncoding
	hdrBytes, err := enc.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("jwt: bad header encoding")
	}
	var hdr struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return nil, errors.New("jwt: bad header json")
	}
	if hdr.Alg != "HS256" {
		return nil, errors.New("jwt: unsupported alg")
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := mac.Sum(nil)
	got, err := enc.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("jwt: bad signature encoding")
	}
	if !hmac.Equal(expected, got) {
		return nil, errors.New("jwt: signature mismatch")
	}
	plBytes, err := enc.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("jwt: bad payload encoding")
	}
	var claims map[string]any
	if err := json.Unmarshal(plBytes, &claims); err != nil {
		return nil, errors.New("jwt: bad payload json")
	}
	return claims, nil
}
