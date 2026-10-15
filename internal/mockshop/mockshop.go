// Package mockshop simulates the documented Shopify endpoints for local tests.
// It is deliberately not a replacement identity provider for production.
package mockshop

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const ClientID = "demo-client"
const Email = "camille@example.test"

type authorization struct{ Challenge, Nonce string }
type Mock struct {
	BaseURL, Callback, Secret, Fault string
	key                              *rsa.PrivateKey
	mu                               sync.Mutex
	codes                            map[string]authorization
	access                           map[string]bool
}

func New(base, callback, secret, fault string) (*Mock, error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Mock{BaseURL: base, Callback: callback, Secret: secret, Fault: fault, key: k,
		codes: map[string]authorization{}, access: map[string]bool{}}, nil
}

func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (m *Mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		respond(w, map[string]any{"issuer": m.BaseURL + "/identity", "authorization_endpoint": m.BaseURL + "/authorize", "token_endpoint": m.BaseURL + "/token", "jwks_uri": m.BaseURL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	case "/.well-known/customer-account-api":
		respond(w, map[string]string{"graphql_api": m.BaseURL + "/customer/api/2026-10/graphql"})
	case "/jwks":
		respond(w, map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "mock-key", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(m.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(m.key.E)).Bytes())}}})
	case "/authorize":
		m.authorize(w, r)
	case "/token":
		m.token(w, r)
	case "/customer/api/2026-10/graphql":
		m.mu.Lock()
		valid := m.access[r.Header.Get("Authorization")]
		m.mu.Unlock()
		if !valid {
			http.Error(w, "invalid access token, expected raw token", 401)
			return
		}
		if m.Fault == "graphql" {
			respond(w, map[string]any{"errors": []any{map[string]string{"message": "customer permission denied"}}})
			return
		}
		respond(w, map[string]any{"data": map[string]any{"customer": map[string]any{"id": "gid://shopify/Customer/12345", "firstName": "Camille", "lastName": "Martin", "emailAddress": map[string]string{"emailAddress": Email}}}})
	case "/login":
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "mock_shop_session", Value: "yes", HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/"})
		resume := r.URL.Query().Get("resume")
		if resume == "" {
			resume = "/"
		}
		if len(resume) < 1 || resume[0] != '/' || (len(resume) > 1 && resume[1] == '/') {
			http.Error(w, "invalid return", 400)
			return
		}
		http.Redirect(w, r, resume, 303)
	case "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		home.Execute(w, map[string]string{"Studio": m.Callback[:len(m.Callback)-len("/callback")]})
	default:
		http.NotFound(w, r)
	}
}

func (m *Mock) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != ClientID || q.Get("redirect_uri") != m.Callback || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("code_challenge") == "" {
		http.Error(w, "invalid authorization request", 400)
		return
	}
	cookie, _ := r.Cookie("mock_shop_session")
	if cookie == nil || cookie.Value != "yes" {
		if q.Get("prompt") == "none" {
			u, _ := url.Parse(m.Callback)
			u.RawQuery = url.Values{"error": {"login_required"}, "state": {q.Get("state")}}.Encode()
			http.Redirect(w, r, u.String(), 302)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		login.Execute(w, "/login?resume="+url.QueryEscape(r.URL.RequestURI()))
		return
	}
	code := random()
	m.mu.Lock()
	m.codes[code] = authorization{q.Get("code_challenge"), q.Get("nonce")}
	m.mu.Unlock()
	u, _ := url.Parse(m.Callback)
	u.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
	http.Redirect(w, r, u.String(), 302)
}

func (m *Mock) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	client := r.Form.Get("client_id")
	if m.Secret != "" {
		id, secret, ok := r.BasicAuth()
		if !ok || secret != m.Secret {
			http.Error(w, "invalid client secret", 401)
			return
		}
		client = id
	}
	if client != ClientID || r.Form.Get("redirect_uri") != m.Callback || r.Form.Get("grant_type") != "authorization_code" {
		http.Error(w, "invalid client", 401)
		return
	}
	m.mu.Lock()
	a, ok := m.codes[r.Form.Get("code")]
	delete(m.codes, r.Form.Get("code"))
	m.mu.Unlock()
	hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(hash[:]) != a.Challenge {
		http.Error(w, "invalid code or PKCE", 400)
		return
	}
	now := time.Now()
	c := jwt.MapClaims{"iss": m.BaseURL + "/identity", "sub": "shopify-customer-sub-12345", "aud": ClientID, "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nonce": a.Nonce, "email": Email, "email_verified": true}
	fault := m.Fault
	if strings.HasPrefix(fault, "numeric:") {
		c["sub"] = json.Number("900719925474099312345")
		fault = strings.TrimPrefix(fault, "numeric:")
	}
	switch fault {
	case "nonce":
		c["nonce"] = "wrong"
	case "audience":
		c["aud"] = "wrong-client"
	case "issuer":
		c["iss"] = m.BaseURL + "/other"
	case "expired":
		c["exp"] = now.Add(-time.Hour).Unix()
	case "not_before":
		c["nbf"] = now.Add(time.Hour).Unix()
	case "subject_number":
		c["sub"] = 12345
	case "audience_number":
		c["aud"] = 12345
	case "email":
		c["email"] = "other@example.test"
	}
	t := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
	if fault == "algorithm" {
		t = jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	}
	t.Header["kid"] = "mock-key"
	var key any = m.key
	if fault == "signature" {
		key, _ = rsa.GenerateKey(rand.Reader, 2048)
	}
	if fault == "algorithm" {
		key = []byte("not-a-shopify-key")
	}
	id, err := t.SignedString(key)
	if err != nil {
		http.Error(w, "signing error", 500)
		return
	}
	access := random()
	m.mu.Lock()
	m.access[access] = true
	m.mu.Unlock()
	respond(w, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600, "id_token": id})
}

var login = template.Must(template.New("login").Parse(`<!doctype html><html lang="fr"><meta charset="utf-8"><title>Simulation Shopify</title><h1>Connexion Shopify simulée</h1><p>Test local uniquement. Aucun vrai code par e-mail.</p><form method="post" action="{{.}}"><button>Connexion de Camille</button></form></html>`))
var home = template.Must(template.New("home").Parse(`<!doctype html><html lang="fr"><meta charset="utf-8"><title>Boutique simulée</title><h1>Boutique simulée</h1><p>Cette page ne contacte pas Shopify.</p><form method="post" action="/login"><button>Connexion de Camille à la boutique</button></form><p><a href="{{.Studio}}">Ouvrir le studio</a></p></html>`))
