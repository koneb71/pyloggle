package webui

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
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

// OpenID Connect login for the UI: the authorization-code flow with PKCE, a signed state
// cookie, discovery with a 24-hour cache, and id_token verification (RS256 and ES256) against
// the provider's JWKS. The Azure preset keeps its fixed v2.0 endpoints, tenant check and
// object-id subject so existing deployments behave exactly as before.

type oauthState struct {
	State     string `json:"state"`
	Verifier  string `json:"verifier"`
	Nonce     string `json:"nonce"`
	ReturnTo  string `json:"return_to"`
	ExpiresAt int64  `json:"exp"`
}

// oidcEndpoints is what discovery (or the Azure preset) provides.
type oidcEndpoints struct {
	Issuer        string `json:"issuer"`
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
	JWKS          string `json:"jwks_uri"`
}

type jwksCache struct {
	mu        sync.Mutex
	fetchedAt time.Time
	keys      map[string]crypto.PublicKey

	epMu        sync.Mutex
	endpoints   oidcEndpoints
	discovered  time.Time
	httpClient  *http.Client
	azureStatic bool
}

func (u *UI) ensureJWKS() {
	if u.jwks != nil {
		return
	}
	u.jwks = &jwksCache{keys: map[string]crypto.PublicKey{}, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

// endpoints returns the provider endpoints: static for Azure, discovered (and cached a day)
// for everything else. The discovered issuer must match the configured one.
func (u *UI) endpoints(ctx context.Context) (oidcEndpoints, error) {
	u.ensureJWKS()
	o := u.cfg.OIDC()
	if o.Azure {
		base := "https://login.microsoftonline.com/" + o.AzureTenant
		return oidcEndpoints{Issuer: o.Issuer, Authorization: base + "/oauth2/v2.0/authorize", Token: base + "/oauth2/v2.0/token", JWKS: base + "/discovery/v2.0/keys"}, nil
	}
	if o.Issuer == "" {
		return oidcEndpoints{}, errors.New("PYLOGGLE_OIDC_ISSUER is not set")
	}
	u.jwks.epMu.Lock()
	defer u.jwks.epMu.Unlock()
	if u.jwks.endpoints.Token != "" && time.Since(u.jwks.discovered) < 24*time.Hour {
		return u.jwks.endpoints, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.Issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oidcEndpoints{}, err
	}
	resp, err := u.jwks.httpClient.Do(req)
	if err != nil {
		return oidcEndpoints{}, fmt.Errorf("oidc discovery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return oidcEndpoints{}, fmt.Errorf("oidc discovery: http %d", resp.StatusCode)
	}
	var ep oidcEndpoints
	if err := json.Unmarshal(b, &ep); err != nil {
		return oidcEndpoints{}, fmt.Errorf("oidc discovery: %w", err)
	}
	if strings.TrimRight(ep.Issuer, "/") != o.Issuer {
		return oidcEndpoints{}, fmt.Errorf("oidc discovery: issuer %q does not match PYLOGGLE_OIDC_ISSUER", ep.Issuer)
	}
	if ep.Authorization == "" || ep.Token == "" || ep.JWKS == "" {
		return oidcEndpoints{}, errors.New("oidc discovery: document lacks authorization, token or jwks endpoints")
	}
	u.jwks.endpoints, u.jwks.discovered = ep, time.Now()
	return ep, nil
}

func (u *UI) oidcRedirectURL(r *http.Request) string {
	if ru := u.cfg.OIDC().RedirectURL; ru != "" {
		return ru
	}
	scheme := "http"
	if isSecureRequest(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/ui/oauth2/callback"
}

func (u *UI) oidcConfigError() string {
	return u.cfg.OIDC().Missing(u.cfg.UISessionSecret)
}

// oidcStart redirects the browser to the provider with PKCE, a nonce and a signed state cookie.
func (u *UI) oidcStart(w http.ResponseWriter, r *http.Request, returnTo string) {
	if msg := u.oidcConfigError(); msg != "" {
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}
	secret, err := u.uiSecret()
	if err != nil {
		http.Error(w, "UI session secret misconfigured: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ep, err := u.endpoints(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
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
	if returnTo == "" || !strings.HasPrefix(returnTo, "/ui/") {
		returnTo = "/ui/issues"
	}
	chal := sha256.Sum256([]byte(verifier))
	st := oauthState{State: state, Verifier: verifier, Nonce: nonce, ReturnTo: returnTo, ExpiresAt: time.Now().Add(uiOAuthStateMaxAge * time.Second).Unix()}
	tok, err := encodeSignedToken(secret, st)
	if err != nil {
		http.Error(w, "failed to sign oauth state", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: uiOAuthStateCookie, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecureRequest(r), MaxAge: uiOAuthStateMaxAge})

	o := u.cfg.OIDC()
	v := url.Values{}
	v.Set("client_id", o.ClientID)
	v.Set("response_type", "code")
	v.Set("redirect_uri", u.oidcRedirectURL(r))
	v.Set("response_mode", "query")
	v.Set("scope", o.Scopes)
	v.Set("state", state)
	v.Set("nonce", nonce)
	v.Set("code_challenge", base64.RawURLEncoding.EncodeToString(chal[:]))
	v.Set("code_challenge_method", "S256")
	sep := "?"
	if strings.Contains(ep.Authorization, "?") {
		sep = "&"
	}
	http.Redirect(w, r, ep.Authorization+sep+v.Encode(), http.StatusFound)
}

func (u *UI) clearOAuthState(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: uiOAuthStateCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecureRequest(r), MaxAge: -1})
}

// oidcCallback finishes the flow: state check, code exchange, id_token verification, allow-list,
// then the signed session cookie.
func (u *UI) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if !u.loginRequired() {
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
	if e := strings.TrimSpace(r.URL.Query().Get("error")); e != "" {
		http.Error(w, "login refused by the identity provider: "+e+" "+r.URL.Query().Get("error_description"), http.StatusUnauthorized)
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
	var st oauthState
	if err := decodeSignedToken(secret, c.Value, &st); err != nil {
		http.Error(w, "bad oauth cookie", http.StatusBadRequest)
		return
	}
	if time.Now().Unix() >= st.ExpiresAt {
		http.Error(w, "oauth state expired", http.StatusBadRequest)
		return
	}
	if !subtleStringEq(st.State, state) {
		http.Error(w, "oauth state mismatch", http.StatusBadRequest)
		return
	}
	ep, err := u.endpoints(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	idToken, err := u.exchangeCode(r.Context(), ep.Token, u.oidcRedirectURL(r), code, st.Verifier)
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	claims, err := u.verifyIDToken(r.Context(), ep, idToken, st.Nonce)
	if err != nil {
		http.Error(w, "id_token verification failed: "+err.Error(), http.StatusUnauthorized)
		return
	}
	if !u.allowedUser(claims) {
		u.clearOAuthState(w, r)
		http.Error(w, "this account ("+claims.Email+") is not allowed to use this pyloggle instance", http.StatusForbidden)
		return
	}
	now := time.Now()
	sess := uiSessionClaims{Sub: claims.Sub, Name: claims.Name, PreferredUsername: claims.PreferredUsername, Email: claims.Email, TenantID: claims.TenantID, Issuer: claims.Issuer,
		IssuedAtUnix: now.Unix(), ExpiresAtUnix: now.Add(uiSessionMaxAge * time.Second).Unix()}
	if err := u.setUISession(w, r, sess); err != nil {
		http.Error(w, "failed to set session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	u.clearOAuthState(w, r)
	http.Redirect(w, r, st.ReturnTo, http.StatusFound)
}

// allowedUser applies the optional e-mail / domain allow-lists.
func (u *UI) allowedUser(c verifiedUser) bool {
	o := u.cfg.OIDC()
	if len(o.AllowedDomains) == 0 && len(o.AllowedEmails) == 0 {
		return true
	}
	email := strings.ToLower(strings.TrimSpace(c.Email))
	for _, e := range o.AllowedEmails {
		if strings.ToLower(strings.TrimSpace(e)) == email && email != "" {
			return true
		}
	}
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return false
	}
	domain := email[at+1:]
	for _, d := range o.AllowedDomains {
		if strings.ToLower(strings.TrimSpace(strings.TrimPrefix(d, "@"))) == domain {
			return true
		}
	}
	return false
}

type idTokenClaims struct {
	Issuer            string `json:"iss"`
	Subject           string `json:"sub"`
	Nonce             string `json:"nonce"`
	TenantID          string `json:"tid"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	UPN               string `json:"upn"`
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

func (u *UI) exchangeCode(ctx context.Context, tokenURL, redirectURL, code, verifier string) (string, error) {
	o := u.cfg.OIDC()
	form := url.Values{}
	form.Set("client_id", o.ClientID)
	form.Set("client_secret", o.ClientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURL)
	form.Set("code_verifier", verifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	u.ensureJWKS()
	resp, err := u.jwks.httpClient.Do(req)
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

// verifyIDToken checks the signature (RS256 or ES256 against the JWKS), issuer, audience,
// nonce and validity window; the Azure preset also checks the tenant and prefers the object id.
func (u *UI) verifyIDToken(ctx context.Context, ep oidcEndpoints, idToken, expectedNonce string) (verifiedUser, error) {
	o := u.cfg.OIDC()
	h, p, sig, signingInput, err := parseJWT(idToken)
	if err != nil {
		return verifiedUser{}, err
	}
	alg, _ := h["alg"].(string)
	if alg != "RS256" && alg != "ES256" {
		return verifiedUser{}, fmt.Errorf("unsupported alg %q", alg)
	}
	kid, _ := h["kid"].(string)
	if kid == "" {
		return verifiedUser{}, errors.New("missing kid")
	}
	var claims idTokenClaims
	if err := json.Unmarshal(p, &claims); err != nil {
		return verifiedUser{}, errors.New("bad claims json")
	}
	now := time.Now().Unix()
	if claims.ExpiresAtUnix > 0 && now >= claims.ExpiresAtUnix {
		return verifiedUser{}, errors.New("token expired")
	}
	if claims.NotBeforeUnix > 0 && now+10 < claims.NotBeforeUnix {
		return verifiedUser{}, errors.New("token not yet valid")
	}
	if strings.TrimRight(strings.TrimSpace(claims.Issuer), "/") != strings.TrimRight(ep.Issuer, "/") {
		return verifiedUser{}, errors.New("issuer mismatch")
	}
	if o.Azure && o.AzureTenant != "" && strings.TrimSpace(claims.TenantID) != o.AzureTenant {
		return verifiedUser{}, errors.New("tenant mismatch")
	}
	if expectedNonce != "" && !subtleStringEq(expectedNonce, claims.Nonce) {
		return verifiedUser{}, errors.New("nonce mismatch")
	}
	if !audMatches(claims.Audience, o.ClientID) {
		return verifiedUser{}, errors.New("aud mismatch")
	}
	pub, err := u.jwksKeyForKID(ctx, ep.JWKS, kid)
	if err != nil {
		return verifiedUser{}, err
	}
	sum := sha256.Sum256(signingInput)
	switch alg {
	case "RS256":
		rk, ok := pub.(*rsa.PublicKey)
		if !ok {
			return verifiedUser{}, errors.New("kid is not an RSA key")
		}
		if err := rsa.VerifyPKCS1v15(rk, crypto.SHA256, sum[:], sig); err != nil {
			return verifiedUser{}, errors.New("bad signature")
		}
	case "ES256":
		ek, ok := pub.(*ecdsa.PublicKey)
		if !ok || len(sig) != 64 {
			return verifiedUser{}, errors.New("kid is not a P-256 key or the signature is malformed")
		}
		rr := new(big.Int).SetBytes(sig[:32])
		ss := new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(ek, sum[:], rr, ss) {
			return verifiedUser{}, errors.New("bad signature")
		}
	}
	sub := strings.TrimSpace(claims.Subject)
	if o.Azure && strings.TrimSpace(claims.Oid) != "" {
		sub = strings.TrimSpace(claims.Oid)
	}
	if sub == "" {
		return verifiedUser{}, errors.New("missing subject")
	}
	email := strings.TrimSpace(claims.Email)
	if email == "" {
		email = strings.TrimSpace(claims.PreferredUsername)
	}
	if email == "" {
		email = strings.TrimSpace(claims.UPN)
	}
	if !strings.Contains(email, "@") {
		email = ""
	}
	return verifiedUser{Sub: sub, Name: strings.TrimSpace(claims.Name), PreferredUsername: strings.TrimSpace(claims.PreferredUsername), Email: email,
		TenantID: strings.TrimSpace(claims.TenantID), Issuer: strings.TrimSpace(claims.Issuer)}, nil
}

func (u *UI) jwksKeyForKID(ctx context.Context, jwksURL, kid string) (crypto.PublicKey, error) {
	u.ensureJWKS()
	u.jwks.mu.Lock()
	defer u.jwks.mu.Unlock()
	if time.Since(u.jwks.fetchedAt) > 6*time.Hour || len(u.jwks.keys) == 0 {
		keys, err := u.fetchJWKS(ctx, jwksURL)
		if err != nil {
			return nil, err
		}
		u.jwks.keys, u.jwks.fetchedAt = keys, time.Now()
	}
	if k, ok := u.jwks.keys[kid]; ok && k != nil {
		return k, nil
	}
	keys, err := u.fetchJWKS(ctx, jwksURL) // the kid may have just rotated
	if err != nil {
		return nil, err
	}
	u.jwks.keys, u.jwks.fetchedAt = keys, time.Now()
	if k, ok := u.jwks.keys[kid]; ok && k != nil {
		return k, nil
	}
	return nil, errors.New("kid not found")
}

// fetchJWKS loads RSA and P-256 keys; other key types are skipped.
func (u *UI) fetchJWKS(ctx context.Context, jwksURL string) (map[string]crypto.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.jwks.httpClient.Do(req)
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
			Crv string `json:"crv"`
			N   string `json:"n"`
			E   string `json:"e"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	out := map[string]crypto.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kid == "" {
			continue
		}
		switch k.Kty {
		case "RSA":
			nBytes, err1 := base64.RawURLEncoding.DecodeString(k.N)
			eBytes, err2 := base64.RawURLEncoding.DecodeString(k.E)
			if err1 != nil || err2 != nil || len(nBytes) == 0 {
				continue
			}
			e := 0
			for _, bb := range eBytes {
				e = e<<8 + int(bb)
			}
			if e == 0 {
				continue
			}
			out[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}
		case "EC":
			if k.Crv != "P-256" {
				continue
			}
			xb, err1 := base64.RawURLEncoding.DecodeString(k.X)
			yb, err2 := base64.RawURLEncoding.DecodeString(k.Y)
			if err1 != nil || err2 != nil {
				continue
			}
			pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(xb), Y: new(big.Int).SetBytes(yb)}
			if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
				continue
			}
			out[k.Kid] = pub
		}
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
	return hdr, pRaw, sig, []byte(parts[0] + "." + parts[1]), nil
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
	}
	return false
}

func subtleStringEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	ok := 0
	for i := 0; i < len(a); i++ {
		ok |= int(a[i] ^ b[i])
	}
	return ok == 0
}
