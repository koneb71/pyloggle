package webui

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/koneb71/pyloggle/internal/config"
)

// fakeProvider is a minimal OpenID Connect provider: discovery, a JWKS with one RSA and one
// P-256 key, and a token endpoint that mints an id_token for the nonce the login redirect sent.
type fakeProvider struct {
	srv    *httptest.Server
	rsaKey *rsa.PrivateKey
	ecKey  *ecdsa.PrivateKey

	mu          sync.Mutex
	alg         string         // RS256 | ES256
	claims      map[string]any // extra claims merged into the id_token
	issuerClaim string         // overrides the iss claim when set
	lastForm    url.Values     // the last token request
	issuerDoc   string         // issuer announced by discovery (defaults to the server URL)
	tokenHits   int
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ek, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{rsaKey: rk, ecKey: ek, alg: "RS256", claims: map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		iss := p.srv.URL
		p.mu.Lock()
		if p.issuerDoc != "" {
			iss = p.issuerDoc
		}
		p.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": iss, "authorization_endpoint": p.srv.URL + "/authorize",
			"token_endpoint": p.srv.URL + "/token", "jwks_uri": p.srv.URL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		b64 := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{
			{"kid": "rsa1", "kty": "RSA", "alg": "RS256", "use": "sig", "n": b64(rk.N.Bytes()), "e": b64(big.NewInt(int64(rk.E)).Bytes())},
			{"kid": "ec1", "kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "x": b64(ek.X.FillBytes(make([]byte, 32))), "y": b64(ek.Y.FillBytes(make([]byte, 32)))},
			{"kid": "weird", "kty": "oct", "k": "AAAA"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		p.mu.Lock()
		p.lastForm = r.PostForm
		p.tokenHits++
		p.mu.Unlock()
		// The fake "code" is "code:<nonce>" so the token can echo the nonce back.
		nonce := strings.TrimPrefix(r.PostForm.Get("code"), "code:")
		if nonce == "" || r.PostForm.Get("code_verifier") == "" || r.PostForm.Get("client_secret") != "sekrit" {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id_token": p.mint(t, nonce, r.PostForm.Get("client_id")), "token_type": "Bearer"})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeProvider) mint(t *testing.T, nonce, aud string) string {
	t.Helper()
	p.mu.Lock()
	alg, extra, iss := p.alg, p.claims, p.issuerClaim
	p.mu.Unlock()
	if iss == "" {
		iss = p.srv.URL
	}
	now := time.Now().Unix()
	claims := map[string]any{"iss": iss, "sub": "user-1", "aud": aud, "nonce": nonce, "iat": now, "nbf": now - 5, "exp": now + 300,
		"name": "Ada Lovelace", "email": "ada@example.com"}
	for k, v := range extra {
		if v == nil {
			delete(claims, k)
		} else {
			claims[k] = v
		}
	}
	kid := "rsa1"
	if alg == "ES256" {
		kid = "ec1"
	}
	hb, _ := json.Marshal(map[string]any{"alg": alg, "kid": kid, "typ": "JWT"})
	pb, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(pb)
	sum := sha256.Sum256([]byte(signing))
	var sig []byte
	switch alg {
	case "RS256":
		s, err := rsa.SignPKCS1v15(rand.Reader, p.rsaKey, crypto.SHA256, sum[:])
		if err != nil {
			t.Fatal(err)
		}
		sig = s
	case "ES256":
		r, s, err := ecdsa.Sign(rand.Reader, p.ecKey, sum[:])
		if err != nil {
			t.Fatal(err)
		}
		sig = append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	default:
		t.Fatalf("unknown alg %s", alg)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func oidcTestUI(p *fakeProvider) *UI {
	cfg := config.Config{UIAuthMode: "oidc", OIDCIssuer: p.srv.URL, OIDCClientID: "pyloggle-ui", OIDCClientSecret: "sekrit",
		OIDCScopes: "openid email", OIDCDisplayName: "Acme SSO", UISessionSecret: "0123456789abcdef0123456789abcdef", DefaultProjID: "default"}
	u := NewForTest(cfg, &fakeStore{}, fakeKeys{}, nil)
	return u
}

// startLogin runs GET /ui/login and returns the state cookie plus the parsed redirect params.
func startLogin(t *testing.T, u *UI) (*http.Cookie, url.Values) {
	t.Helper()
	rec := httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/login?next=%2Fui%2Flogs", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("login: code=%d body=%s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	var state *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == uiOAuthStateCookie && c.Value != "" {
			state = c
		}
	}
	if state == nil {
		t.Fatalf("no state cookie set; cookies=%v", rec.Result().Cookies())
	}
	return state, loc.Query()
}

// finishLogin calls the callback with the code the fake provider understands and returns the response.
func finishLogin(t *testing.T, u *UI, state *http.Cookie, q url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/ui/oauth2/callback?code=code:"+url.QueryEscape(q.Get("nonce"))+"&state="+url.QueryEscape(q.Get("state")), nil)
	req.AddCookie(state)
	rec := httptest.NewRecorder()
	u.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == uiSessionCookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestOIDCLoginRoundTrip(t *testing.T) {
	for _, alg := range []string{"RS256", "ES256"} {
		t.Run(alg, func(t *testing.T) {
			p := newFakeProvider(t)
			p.alg = alg
			u := oidcTestUI(p)

			state, q := startLogin(t, u)
			if q.Get("client_id") != "pyloggle-ui" || q.Get("scope") != "openid email" || q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("state") == "" {
				t.Fatalf("authorize params: %v", q)
			}
			if q.Get("redirect_uri") != "http://example.com/ui/oauth2/callback" {
				t.Fatalf("redirect_uri=%q", q.Get("redirect_uri"))
			}

			rec := finishLogin(t, u, state, q)
			if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/ui/logs" {
				t.Fatalf("callback: code=%d loc=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
			}
			sess := sessionCookie(rec)
			if sess == nil {
				t.Fatal("no session cookie")
			}
			// PKCE: the verifier sent to the token endpoint hashes to the challenge from the redirect.
			sum := sha256.Sum256([]byte(p.lastForm.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != q.Get("code_challenge") {
				t.Fatalf("code_verifier does not match the challenge")
			}
			if p.lastForm.Get("redirect_uri") != q.Get("redirect_uri") || p.lastForm.Get("grant_type") != "authorization_code" {
				t.Fatalf("token form: %v", p.lastForm)
			}

			// The session now carries the user and /ui/api/me shows the provider name.
			req := httptest.NewRequest(http.MethodGet, "/ui/api/me", nil)
			req.AddCookie(sess)
			rec = httptest.NewRecorder()
			u.ServeHTTP(rec, req)
			var me meResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || rec.Code != 200 {
				t.Fatalf("me: code=%d body=%s", rec.Code, rec.Body.String())
			}
			if me.AuthMode != "oidc" || me.Features.AuthProvider != "Acme SSO" || me.User == nil || me.User.Sub != "user-1" || me.User.Email != "ada@example.com" || me.User.Name != "Ada Lovelace" {
				t.Fatalf("me: %+v user=%+v", me, me.User)
			}
		})
	}
}

func TestOIDCCallbackRejectsBadStateAndNonce(t *testing.T) {
	p := newFakeProvider(t)
	u := oidcTestUI(p)
	state, q := startLogin(t, u)

	// Wrong state parameter.
	req := httptest.NewRequest(http.MethodGet, "/ui/oauth2/callback?code=code:"+url.QueryEscape(q.Get("nonce"))+"&state=other", nil)
	req.AddCookie(state)
	rec := httptest.NewRecorder()
	u.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "state mismatch") {
		t.Fatalf("state mismatch: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// No state cookie at all.
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/oauth2/callback?code=x&state="+url.QueryEscape(q.Get("state")), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing cookie: code=%d", rec.Code)
	}
	// Token with another nonce (the fake provider echoes the nonce from the code).
	req = httptest.NewRequest(http.MethodGet, "/ui/oauth2/callback?code=code:stale-nonce&state="+url.QueryEscape(q.Get("state")), nil)
	req.AddCookie(state)
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "nonce mismatch") {
		t.Fatalf("nonce: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// Provider-side error.
	req = httptest.NewRequest(http.MethodGet, "/ui/oauth2/callback?error=access_denied&error_description=nope", nil)
	req.AddCookie(state)
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "access_denied") {
		t.Fatalf("provider error: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOIDCRejectsIssuerAudienceAndAlg(t *testing.T) {
	p := newFakeProvider(t)
	u := oidcTestUI(p)

	// iss claim from another issuer.
	p.issuerClaim = "https://evil.example"
	state, q := startLogin(t, u)
	rec := finishLogin(t, u, state, q)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "issuer mismatch") {
		t.Fatalf("issuer: code=%d body=%s", rec.Code, rec.Body.String())
	}
	p.issuerClaim = ""

	// aud for a different client.
	p.claims = map[string]any{"aud": []any{"someone-else"}}
	state, q = startLogin(t, u)
	rec = finishLogin(t, u, state, q)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "aud mismatch") {
		t.Fatalf("aud: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// aud as a list that contains us is fine.
	p.claims = map[string]any{"aud": []any{"someone-else", "pyloggle-ui"}}
	state, q = startLogin(t, u)
	if rec = finishLogin(t, u, state, q); rec.Code != http.StatusFound {
		t.Fatalf("aud list: code=%d body=%s", rec.Code, rec.Body.String())
	}
	p.claims = map[string]any{}

	// Expired token.
	p.claims = map[string]any{"exp": time.Now().Unix() - 10}
	state, q = startLogin(t, u)
	if rec = finishLogin(t, u, state, q); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("exp: code=%d body=%s", rec.Code, rec.Body.String())
	}
	p.claims = map[string]any{}

	// Unsupported alg is refused before any signature work.
	u2 := oidcTestUI(p)
	ep, err := u2.endpoints(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	hb, _ := json.Marshal(map[string]any{"alg": "HS256", "kid": "rsa1"})
	pb, _ := json.Marshal(map[string]any{"iss": p.srv.URL, "aud": "pyloggle-ui", "sub": "x"})
	tok := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(pb) + "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))
	if _, err := u2.verifyIDToken(t.Context(), ep, tok, ""); err == nil || !strings.Contains(err.Error(), "unsupported alg") {
		t.Fatalf("HS256 accepted: %v", err)
	}
	// A token signed with the RSA key but announced as ES256 under the EC kid fails the signature.
	p.alg = "RS256"
	good := p.mint(t, "", "pyloggle-ui")
	parts := strings.Split(good, ".")
	hb, _ = json.Marshal(map[string]any{"alg": "ES256", "kid": "ec1"})
	forged := base64.RawURLEncoding.EncodeToString(hb) + "." + parts[1] + "." + parts[2]
	if _, err := u2.verifyIDToken(t.Context(), ep, forged, ""); err == nil {
		t.Fatal("forged alg/kid accepted")
	}
}

func TestOIDCAllowListsAndEmailFallbacks(t *testing.T) {
	p := newFakeProvider(t)
	u := oidcTestUI(p)
	u.cfg.OIDCAllowedDomains = []string{"@Example.com"}

	state, q := startLogin(t, u)
	if rec := finishLogin(t, u, state, q); rec.Code != http.StatusFound {
		t.Fatalf("allowed domain: code=%d body=%s", rec.Code, rec.Body.String())
	}
	p.claims = map[string]any{"email": "mallory@other.org"}
	state, q = startLogin(t, u)
	rec := finishLogin(t, u, state, q)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "mallory@other.org") || sessionCookie(rec) != nil {
		t.Fatalf("other domain: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// An explicit e-mail allow-list entry wins over the domain rule.
	u.cfg.OIDCAllowedEmails = []string{"Mallory@other.org"}
	state, q = startLogin(t, u)
	if rec = finishLogin(t, u, state, q); rec.Code != http.StatusFound {
		t.Fatalf("allowed e-mail: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// No email claim: preferred_username, then upn, feed the e-mail used by the allow-list and /me.
	u.cfg.OIDCAllowedEmails = nil
	p.claims = map[string]any{"email": nil, "preferred_username": "grace@example.com"}
	state, q = startLogin(t, u)
	rec = finishLogin(t, u, state, q)
	if rec.Code != http.StatusFound {
		t.Fatalf("preferred_username: code=%d body=%s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/ui/api/me", nil)
	req.AddCookie(sessionCookie(rec))
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, req)
	var me meResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.User == nil || me.User.Email != "grace@example.com" {
		t.Fatalf("me after preferred_username: %s", rec.Body.String())
	}
	p.claims = map[string]any{"email": nil, "upn": "linus@example.com"}
	state, q = startLogin(t, u)
	if rec = finishLogin(t, u, state, q); rec.Code != http.StatusFound {
		t.Fatalf("upn: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// A bare username (no @) is not an e-mail: the domain rule refuses it.
	p.claims = map[string]any{"email": nil, "preferred_username": "nobody"}
	state, q = startLogin(t, u)
	if rec = finishLogin(t, u, state, q); rec.Code != http.StatusForbidden {
		t.Fatalf("bare username: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOIDCDiscoveryIssuerMustMatch(t *testing.T) {
	p := newFakeProvider(t)
	p.issuerDoc = "https://other.example"
	u := oidcTestUI(p)
	rec := httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/login", nil))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "does not match") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	// A trailing slash on the configured issuer is tolerated.
	p.issuerDoc = p.srv.URL + "/"
	u = oidcTestUI(p)
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/login", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("trailing slash: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOIDCAdminsAndLogout(t *testing.T) {
	p := newFakeProvider(t)
	u := oidcTestUI(p)
	u.cfg.UIAdmins = []string{"ada@example.com"}
	state, q := startLogin(t, u)
	rec := finishLogin(t, u, state, q)
	sess := sessionCookie(rec)
	if sess == nil {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/ui/api/me", nil)
	req.AddCookie(sess)
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, req)
	var me meResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if !me.IsAdmin {
		t.Fatalf("expected admin: %s", rec.Body.String())
	}
	// A different account is not an admin.
	p.claims = map[string]any{"email": "bob@example.com", "sub": "user-2"}
	state, q = startLogin(t, u)
	rec = finishLogin(t, u, state, q)
	req = httptest.NewRequest(http.MethodGet, "/ui/api/me", nil)
	req.AddCookie(sessionCookie(rec))
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.IsAdmin {
		t.Fatalf("bob is not an admin: %s", rec.Body.String())
	}
	// Logout clears the session and sends the browser back to the login page.
	req = httptest.NewRequest(http.MethodGet, "/ui/logout", nil)
	req.AddCookie(sess)
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/ui/login" {
		t.Fatalf("logout: code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == uiSessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("session cookie not cleared: %v", rec.Result().Cookies())
	}
}

func TestOIDCConfigErrors(t *testing.T) {
	u := NewForTest(config.Config{UIAuthMode: "oidc", UISessionSecret: "0123456789abcdef0123456789abcdef"}, &fakeStore{}, fakeKeys{}, nil)
	rec := httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/login", nil))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "PYLOGGLE_OIDC_ISSUER") {
		t.Fatalf("unconfigured oidc: code=%d body=%s", rec.Code, rec.Body.String())
	}
	u = NewForTest(config.Config{UIAuthMode: "oidc", OIDCIssuer: "https://x", OIDCClientID: "c", OIDCClientSecret: "s"}, &fakeStore{}, fakeKeys{}, nil)
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/login", nil))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "PYLOGGLE_UI_SESSION_SECRET") {
		t.Fatalf("missing secret: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// The callback is a 404 when no login is configured.
	u = NewForTest(config.Config{UIAuthMode: "disabled"}, &fakeStore{}, fakeKeys{}, nil)
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/oauth2/callback?code=a&state=b", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("callback in disabled mode: %d", rec.Code)
	}
}

func TestAzurePresetDerivesSettings(t *testing.T) {
	cfg := config.Config{UIAuthMode: "azure", AzureTenantID: "tenant-1", AzureClientID: "app", AzureClientSecret: "s", AzureRedirectURL: "https://logs.example/ui/oauth2/callback",
		OIDCAllowedDomains: []string{"example.com"}}
	o := cfg.OIDC()
	if !o.Enabled() || !o.Azure || o.Mode != "azure" || o.Issuer != "https://login.microsoftonline.com/tenant-1/v2.0" || o.ClientID != "app" || o.DisplayName != "Microsoft" || o.RedirectURL != cfg.AzureRedirectURL || len(o.AllowedDomains) != 1 {
		t.Fatalf("azure preset: %+v", o)
	}
	u := NewForTest(cfg, &fakeStore{}, fakeKeys{}, nil)
	ep, err := u.endpoints(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if ep.Authorization != "https://login.microsoftonline.com/tenant-1/oauth2/v2.0/authorize" || ep.Token != "https://login.microsoftonline.com/tenant-1/oauth2/v2.0/token" || ep.JWKS != "https://login.microsoftonline.com/tenant-1/discovery/v2.0/keys" {
		t.Fatalf("azure endpoints: %+v", ep)
	}

	// Azure id_tokens: the tenant must match and the object id becomes the subject.
	p := newFakeProvider(t)
	u.jwks = &jwksCache{keys: map[string]crypto.PublicKey{}, httpClient: p.srv.Client()}
	ep.Issuer, ep.JWKS = p.srv.URL, p.srv.URL+"/keys"
	p.claims = map[string]any{"tid": "tenant-1", "oid": "object-9", "email": nil, "preferred_username": "ada@example.com"}
	user, err := u.verifyIDToken(t.Context(), ep, p.mint(t, "n", "app"), "n")
	if err != nil || user.Sub != "object-9" || user.Email != "ada@example.com" || user.TenantID != "tenant-1" {
		t.Fatalf("azure claims: %+v err=%v", user, err)
	}
	p.claims = map[string]any{"tid": "tenant-2", "oid": "object-9"}
	if _, err := u.verifyIDToken(t.Context(), ep, p.mint(t, "n", "app"), "n"); err == nil || !strings.Contains(err.Error(), "tenant mismatch") {
		t.Fatalf("tenant mismatch expected, got %v", err)
	}

	// The generic mode ignores tid/oid: the subject stays sub.
	g := oidcTestUI(p)
	gep, _ := g.endpoints(t.Context())
	p.claims = map[string]any{"tid": "tenant-2", "oid": "object-9"}
	user, err = g.verifyIDToken(t.Context(), gep, p.mint(t, "n", "pyloggle-ui"), "n")
	if err != nil || user.Sub != "user-1" {
		t.Fatalf("generic ignores oid: %+v err=%v", user, err)
	}

	if d := (config.Config{UIAuthMode: "disabled"}).OIDC(); d.Enabled() || d.Mode != "disabled" {
		t.Fatalf("disabled: %+v", d)
	}
	if d := (config.Config{UIAuthMode: "oidc", OIDCIssuer: "https://x"}).OIDC(); d.DisplayName != "SSO" || d.Scopes != "openid profile email" {
		t.Fatalf("oidc defaults: %+v", d)
	}
}
