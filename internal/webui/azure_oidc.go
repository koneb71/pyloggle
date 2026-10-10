package webui

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type azureOAuthState struct {
	State     string `json:"state"`
	Verifier  string `json:"verifier"`
	Nonce     string `json:"nonce"`
	ReturnTo  string `json:"return_to"`
	ExpiresAt int64  `json:"exp"`
}

type jwksCache struct {
	mu        sync.Mutex
	fetchedAt time.Time
	keys      map[string]*rsa.PublicKey
}

func (u *UI) ensureJWKS() {
	if u.jwks != nil {
		return
	}
	u.jwks = &jwksCache{keys: map[string]*rsa.PublicKey{}}
}

func (u *UI) azureIssuer() string {
	tid := strings.TrimSpace(u.cfg.AzureTenantID)
	return "https://login.microsoftonline.com/" + tid + "/v2.0"
}

func (u *UI) azureAuthorizeURL() string {
	tid := strings.TrimSpace(u.cfg.AzureTenantID)
	return "https://login.microsoftonline.com/" + tid + "/oauth2/v2.0/authorize"
}

func (u *UI) azureTokenURL() string {
	tid := strings.TrimSpace(u.cfg.AzureTenantID)
	return "https://login.microsoftonline.com/" + tid + "/oauth2/v2.0/token"
}

func (u *UI) azureJWKSURL() string {
	tid := strings.TrimSpace(u.cfg.AzureTenantID)
	return "https://login.microsoftonline.com/" + tid + "/discovery/v2.0/keys"
}

func (u *UI) azureRedirectURL(r *http.Request) string {
	if strings.TrimSpace(u.cfg.AzureRedirectURL) != "" {
		return strings.TrimSpace(u.cfg.AzureRedirectURL)
	}
	scheme := "http"
	if isSecureRequest(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/ui/oauth2/callback"
}

func (u *UI) azureStart(w http.ResponseWriter, r *http.Request, returnTo string) {
	// Config validation (fail closed).
	if strings.TrimSpace(u.cfg.AzureTenantID) == "" ||
		strings.TrimSpace(u.cfg.AzureClientID) == "" ||
		strings.TrimSpace(u.cfg.AzureClientSecret) == "" ||
		strings.TrimSpace(u.cfg.UISessionSecret) == "" {
		http.Error(w, "Azure UI auth is enabled but not configured. Set PYLOGGLE_AZURE_TENANT_ID, PYLOGGLE_AZURE_CLIENT_ID, PYLOGGLE_AZURE_CLIENT_SECRET, PYLOGGLE_UI_SESSION_SECRET.", http.StatusInternalServerError)
		return
	}

	secret, err := u.uiSecret()
	if err != nil {
		http.Error(w, "UI session secret misconfigured: "+err.Error(), http.StatusInternalServerError)
		return
	}

	state, err := newRandomB64(32)
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	verifier, err := newRandomB64(48)
	if err != nil {
		http.Error(w, "failed to generate verifier", http.StatusInternalServerError)
		return
	}
	nonce, err := newRandomB64(24)
	if err != nil {
		http.Error(w, "failed to generate nonce", http.StatusInternalServerError)
		return
	}
	if returnTo == "" {
		returnTo = "/ui/issues"
	}
	if !strings.HasPrefix(returnTo, "/ui/") {
		returnTo = "/ui/issues"
	}

	chal := sha256.Sum256([]byte(verifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(chal[:])

	st := azureOAuthState{
		State:     state,
		Verifier:  verifier,
		Nonce:     nonce,
		ReturnTo:  returnTo,
		ExpiresAt: time.Now().Add(uiOAuthStateMaxAge * time.Second).Unix(),
	}
	tok, err := encodeSignedToken(secret, st)
	if err != nil {
		http.Error(w, "failed to sign oauth state", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     uiOAuthStateCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		MaxAge:   uiOAuthStateMaxAge,
	})

	v := url.Values{}
	v.Set("client_id", strings.TrimSpace(u.cfg.AzureClientID))
	v.Set("response_type", "code")
	v.Set("redirect_uri", u.azureRedirectURL(r))
	v.Set("response_mode", "query")
	v.Set("scope", "openid profile email")
	v.Set("state", state)
	v.Set("nonce", nonce)
	v.Set("code_challenge", codeChallenge)
	v.Set("code_challenge_method", "S256")

	http.Redirect(w, r, u.azureAuthorizeURL()+"?"+v.Encode(), http.StatusFound)
}

func (u *UI) clearAzureOAuthState(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiOAuthStateCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		MaxAge:   -1,
	})
}

func (u *UI) azureCallback(w http.ResponseWriter, r *http.Request) {
	if strings.ToLower(strings.TrimSpace(u.cfg.UIAuthMode)) != "azure" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secret, err := u.uiSecret()
	if err != nil {
		http.Error(w, "UI session secret misconfigured: "+err.Error(), http.StatusInternalServerError)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		http.Error(w, "missing code/state", http.StatusBadRequest)
		return
	}
	c, err := r.Cookie(uiOAuthStateCookie)
	if err != nil || strings.TrimSpace(c.Value) == "" {
		http.Error(w, "missing oauth cookie", http.StatusBadRequest)
		return
	}
	var st azureOAuthState
	if err := decodeSignedToken(secret, c.Value, &st); err != nil {
		http.Error(w, "bad oauth cookie", http.StatusBadRequest)
		return
	}
	if time.Now().Unix() >= st.ExpiresAt {
		http.Error(w, "oauth state expired", http.StatusBadRequest)
		return
	}
	if subtleStringEq(st.State, state) == false {
		http.Error(w, "oauth state mismatch", http.StatusBadRequest)
		return
	}

	idToken, err := u.azureExchangeCode(r.Context(), r, code, st.Verifier)
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	claims, err := u.azureVerifyIDToken(r.Context(), idToken, st.Nonce)
	if err != nil {
		http.Error(w, "id_token verification failed: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Create signed UI session cookie.
	now := time.Now()
	sess := uiSessionClaims{
		Sub:               claims.Sub,
		Name:              claims.Name,
		PreferredUsername: claims.PreferredUsername,
		Email:             claims.Email,
		TenantID:          claims.TenantID,
		Issuer:            claims.Issuer,
		IssuedAtUnix:      now.Unix(),
		ExpiresAtUnix:     now.Add(uiSessionMaxAge * time.Second).Unix(),
	}
	if err := u.setUISession(w, r, sess); err != nil {
		http.Error(w, "failed to set session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	u.clearAzureOAuthState(w, r)
	http.Redirect(w, r, st.ReturnTo, http.StatusFound)
}

type azureIDTokenClaims struct {
	Issuer            string `json:"iss"`
	Subject           string `json:"sub"`
	Nonce             string `json:"nonce"`
	TenantID          string `json:"tid"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	Oid               string `json:"oid"`
	Audience          any    `json:"aud"`
	ExpiresAtUnix     int64  `json:"exp"`
	NotBeforeUnix     int64  `json:"nbf"`
	IssuedAtUnix      int64  `json:"iat"`
}

type verifiedUser struct {
	Sub               string
	Name              string
	PreferredUsername string
	Email             string
	TenantID          string
	Issuer            string
}

func (u *UI) azureExchangeCode(ctx context.Context, r *http.Request, code, verifier string) (string, error) {
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(u.cfg.AzureClientID))
	form.Set("client_secret", strings.TrimSpace(u.cfg.AzureClientSecret))
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", u.azureRedirectURL(r))
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.azureTokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.IDToken) == "" {
		return "", errors.New("missing id_token")
	}
	return out.IDToken, nil
}

func (u *UI) azureVerifyIDToken(ctx context.Context, idToken string, expectedNonce string) (verifiedUser, error) {
	h, p, sig, signingInput, err := parseJWT(idToken)
	if err != nil {
		return verifiedUser{}, err
	}
	alg, _ := h["alg"].(string)
	if alg != "RS256" {
		return verifiedUser{}, fmt.Errorf("unsupported alg %q", alg)
	}
	kid, _ := h["kid"].(string)
	if kid == "" {
		return verifiedUser{}, errors.New("missing kid")
	}

	var claims azureIDTokenClaims
	if err := json.Unmarshal(p, &claims); err != nil {
		return verifiedUser{}, errors.New("bad claims json")
	}

	// Basic claim checks.
	now := time.Now().Unix()
	if claims.ExpiresAtUnix > 0 && now >= claims.ExpiresAtUnix {
		return verifiedUser{}, errors.New("token expired")
	}
	if claims.NotBeforeUnix > 0 && now+10 < claims.NotBeforeUnix {
		return verifiedUser{}, errors.New("token not yet valid")
	}
	if strings.TrimSpace(claims.Issuer) != u.azureIssuer() {
		return verifiedUser{}, errors.New("issuer mismatch")
	}
	if strings.TrimSpace(u.cfg.AzureTenantID) != "" && strings.TrimSpace(claims.TenantID) != strings.TrimSpace(u.cfg.AzureTenantID) {
		return verifiedUser{}, errors.New("tenant mismatch")
	}
	if expectedNonce != "" && subtleStringEq(expectedNonce, claims.Nonce) == false {
		return verifiedUser{}, errors.New("nonce mismatch")
	}
	if !audMatches(claims.Audience, strings.TrimSpace(u.cfg.AzureClientID)) {
		return verifiedUser{}, errors.New("aud mismatch")
	}

	pub, err := u.jwksKeyForKID(ctx, kid)
	if err != nil {
		return verifiedUser{}, err
	}
	sum := sha256.Sum256(signingInput)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return verifiedUser{}, errors.New("bad signature")
	}

	sub := strings.TrimSpace(claims.Subject)
	if strings.TrimSpace(claims.Oid) != "" {
		sub = strings.TrimSpace(claims.Oid)
	}
	if sub == "" {
		return verifiedUser{}, errors.New("missing subject")
	}

	return verifiedUser{
		Sub:               sub,
		Name:              strings.TrimSpace(claims.Name),
		PreferredUsername: strings.TrimSpace(claims.PreferredUsername),
		Email:             strings.TrimSpace(claims.Email),
		TenantID:          strings.TrimSpace(claims.TenantID),
		Issuer:            strings.TrimSpace(claims.Issuer),
	}, nil
}

func (u *UI) jwksKeyForKID(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	u.ensureJWKS()
	u.jwks.mu.Lock()
	defer u.jwks.mu.Unlock()

	// Refresh every 6h (kid rotation can happen).
	if time.Since(u.jwks.fetchedAt) > 6*time.Hour || len(u.jwks.keys) == 0 {
		keys, err := u.fetchJWKS(ctx)
		if err != nil {
			return nil, err
		}
		u.jwks.keys = keys
		u.jwks.fetchedAt = time.Now()
	}
	if k, ok := u.jwks.keys[kid]; ok && k != nil {
		return k, nil
	}
	// One more refresh in case kid just rotated.
	keys, err := u.fetchJWKS(ctx)
	if err != nil {
		return nil, err
	}
	u.jwks.keys = keys
	u.jwks.fetchedAt = time.Now()
	if k, ok := u.jwks.keys[kid]; ok && k != nil {
		return k, nil
	}
	return nil, errors.New("kid not found")
}

func (u *UI) fetchJWKS(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.azureJWKSURL(), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("jwks http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	out := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" || k.N == "" || k.E == "" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		n := new(big.Int).SetBytes(nBytes)
		e := 0
		for _, bb := range eBytes {
			e = e<<8 + int(bb)
		}
		if e == 0 {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{N: n, E: e}
	}
	if len(out) == 0 {
		return nil, errors.New("no jwks keys parsed")
	}
	return out, nil
}

func parseJWT(token string) (map[string]any, []byte, []byte, []byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, nil, nil, nil, errors.New("bad jwt format")
	}
	hRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, nil, nil, nil, errors.New("bad jwt header")
	}
	pRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, nil, nil, nil, errors.New("bad jwt payload")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, nil, nil, nil, errors.New("bad jwt sig")
	}
	var hdr map[string]any
	if err := json.Unmarshal(hRaw, &hdr); err != nil {
		return nil, nil, nil, nil, errors.New("bad jwt header json")
	}
	signingInput := []byte(parts[0] + "." + parts[1])
	return hdr, pRaw, sig, signingInput, nil
}

func audMatches(a any, want string) bool {
	if want == "" {
		return false
	}
	switch v := a.(type) {
	case string:
		return subtleStringEq(strings.TrimSpace(v), want)
	case []any:
		for _, it := range v {
			if s, ok := it.(string); ok && subtleStringEq(strings.TrimSpace(s), want) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func subtleStringEq(a, b string) bool {
	// Avoid importing internal/auth subtle helpers; this is small and local.
	if len(a) != len(b) {
		return false
	}
	ok := 0
	for i := 0; i < len(a); i++ {
		ok |= int(a[i] ^ b[i])
	}
	return ok == 0
}
