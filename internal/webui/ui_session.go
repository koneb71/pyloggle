package webui

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	uiSessionCookieName  = "pyloggle_ui_session"
	uiOAuthStateCookie   = "pyloggle_ui_oauth"
	uiSessionMaxAge      = 60 * 60 * 8 // 8h
	uiOAuthStateMaxAge   = 60 * 10     // 10m
	uiCookieTokenVersion = "v1"
	uiCookieTokenParts   = 3
)

type uiSessionClaims struct {
	Sub               string `json:"sub"`
	Name              string `json:"name,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Email             string `json:"email,omitempty"`
	TenantID          string `json:"tid,omitempty"`
	Issuer            string `json:"iss,omitempty"`
	IssuedAtUnix      int64  `json:"iat"`
	ExpiresAtUnix     int64  `json:"exp"`
}

type ctxKeyUIUser struct{}

func withUIUser(ctx context.Context, u uiSessionClaims) context.Context {
	return context.WithValue(ctx, ctxKeyUIUser{}, u)
}

func uiUserFromContext(ctx context.Context) (uiSessionClaims, bool) {
	v := ctx.Value(ctxKeyUIUser{})
	uu, ok := v.(uiSessionClaims)
	return uu, ok
}

func (u *UI) uiSecret() ([]byte, error) {
	raw := strings.TrimSpace(u.cfg.UISessionSecret)
	if raw == "" {
		return nil, errors.New("PYLOGGLE_UI_SESSION_SECRET is required when PYLOGGLE_UI_AUTH is azure or oidc")
	}
	// Support raw secrets and base64 secrets (common for env vars).
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) >= 16 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(raw); err == nil && len(b) >= 16 {
		return b, nil
	}
	if len(raw) < 32 {
		return nil, errors.New("PYLOGGLE_UI_SESSION_SECRET must be at least 32 chars (or base64 for >=16 bytes)")
	}
	return []byte(raw), nil
}

func newRandomB64(n int) (string, error) {
	if n <= 0 {
		n = 32
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func signPayload(secret, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func encodeSignedToken(secret []byte, v any) (string, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sig := signPayload(secret, payload)
	return uiCookieTokenVersion + "." +
		base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sig), nil
}

func decodeSignedToken[T any](secret []byte, token string, out *T) error {
	parts := strings.Split(token, ".")
	if len(parts) != uiCookieTokenParts || parts[0] != uiCookieTokenVersion {
		return errors.New("bad token format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("bad payload encoding")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return errors.New("bad sig encoding")
	}
	want := signPayload(secret, payload)
	if !hmac.Equal(sig, want) {
		return errors.New("bad signature")
	}
	return json.Unmarshal(payload, out)
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	xfp := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")))
	return xfp == "https"
}

func (u *UI) setUISession(w http.ResponseWriter, r *http.Request, claims uiSessionClaims) error {
	secret, err := u.uiSecret()
	if err != nil {
		return err
	}
	tok, err := encodeSignedToken(secret, claims)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     uiSessionCookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		MaxAge:   uiSessionMaxAge,
	})
	return nil
}

func (u *UI) clearUISession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiSessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		MaxAge:   -1,
	})
}

func (u *UI) getUISession(r *http.Request) (uiSessionClaims, bool) {
	c, err := r.Cookie(uiSessionCookieName)
	if err != nil || strings.TrimSpace(c.Value) == "" {
		return uiSessionClaims{}, false
	}
	secret, err := u.uiSecret()
	if err != nil {
		return uiSessionClaims{}, false
	}
	var claims uiSessionClaims
	if err := decodeSignedToken(secret, c.Value, &claims); err != nil {
		return uiSessionClaims{}, false
	}
	now := time.Now().Unix()
	if claims.ExpiresAtUnix > 0 && now >= claims.ExpiresAtUnix {
		return uiSessionClaims{}, false
	}
	if claims.Sub == "" {
		return uiSessionClaims{}, false
	}
	return claims, true
}
