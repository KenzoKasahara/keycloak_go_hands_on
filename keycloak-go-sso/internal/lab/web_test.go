package lab

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// TestCallbackRejectsWrongNonce は、nonceが異なるID Tokenでセッションが作られないことを確認する。
func TestCallbackRejectsWrongNonce(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	// 偽のIdP。/jwks は公開鍵を返し、それ以外はtoken endpointとして誤ったnonceのID Tokenを返す。
	var issuer string
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/jwks" {
			JSON(writer, map[string]any{"keys": []any{map[string]string{
				"kty": "RSA",
				"kid": "test",
				"alg": "RS256",
				"use": "sig",
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}}})
			return
		}

		if request.ParseForm() != nil || request.FormValue("code_verifier") != "expected-verifier" {
			http.Error(writer, "PKCE", 400)
			return
		}

		token := signed(t, key, "RS256", "test", map[string]any{
			"iss":   issuer,
			"aud":   "web-a",
			"sub":   "alice",
			"nonce": "wrong-nonce",
			"exp":   time.Now().Add(time.Minute).Unix(),
		})
		JSON(writer, map[string]any{
			"access_token": "unused-test-token",
			"token_type":   "Bearer",
			"id_token":     token,
		})
	}))
	defer source.Close()
	issuer = source.URL

	ctx := oidc.ClientContext(context.Background(), source.Client())
	app := &web{
		id:     "web-a",
		client: source.Client(),
		pending: map[string]pending{
			"state": {
				Nonce:    "expected-nonce",
				Verifier: "expected-verifier",
				Until:    time.Now().Add(time.Minute),
			},
		},
		sessions: map[string]session{},
		config: oauth2.Config{
			ClientID: "web-a",
			Endpoint: oauth2.Endpoint{
				TokenURL:  source.URL + "/token",
				AuthStyle: oauth2.AuthStyleInParams,
			},
		},
		verifier: oidc.NewVerifier(
			issuer,
			oidc.NewRemoteKeySet(ctx, source.URL+"/jwks"),
			&oidc.Config{ClientID: "web-a", SupportedSigningAlgs: []string{"RS256"}},
		),
	}

	request := httptest.NewRequest("GET", "/callback?state=state&code=code", nil)
	request.AddCookie(&http.Cookie{Name: app.cookieName() + "_flow", Value: "state"})

	recorder := httptest.NewRecorder()
	app.callback(recorder, request)
	if recorder.Code != 401 || len(app.sessions) != 0 {
		t.Fatal("wrong nonce created a session")
	}

	// 使用済みのstateは再利用できない。
	replay := httptest.NewRecorder()
	app.callback(replay, request)
	if replay.Code != 400 {
		t.Fatal("state replay accepted")
	}
}

// TestLoginPKCEAndState は認可要求のPKCE/state/nonceと、不正・期限切れのコールバック拒否を確認する。
func TestLoginPKCEAndState(t *testing.T) {
	app := &web{
		id:      "web-a",
		pending: map[string]pending{},
		config: oauth2.Config{
			ClientID:    "web-a",
			RedirectURL: "http://127.0.0.1:9081/callback",
			Endpoint:    oauth2.Endpoint{AuthURL: "https://issuer.example/auth"},
		},
	}

	recorder := httptest.NewRecorder()
	app.login(recorder, httptest.NewRequest("GET", "/login", nil))
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}

	// 認可要求にPKCE/state/nonceが含まれ、challengeが保存したverifierと対応する。
	query := location.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("nonce") == "" || query.Get("state") == "" {
		t.Fatal("missing PKCE/state/nonce")
	}
	stored := app.pending[query.Get("state")]
	if query.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(stored.Verifier) {
		t.Fatal("incorrect challenge")
	}

	// Cookieと結び付いていないコールバックは拒否される。
	invalid := httptest.NewRecorder()
	app.callback(invalid, httptest.NewRequest("GET", "/callback?state=wrong&code=code", nil))
	if invalid.Code != 400 {
		t.Fatal("unbound callback accepted")
	}

	// 期限切れのstateは拒否され、かつ保存値からも消える。
	expiredState := "expired"
	app.pending[expiredState] = pending{Until: time.Now().Add(-time.Minute)}

	request := httptest.NewRequest("GET", "/callback?state=expired&code=code", nil)
	request.AddCookie(&http.Cookie{Name: app.cookieName() + "_flow", Value: expiredState})

	rejected := httptest.NewRecorder()
	app.callback(rejected, request)
	if rejected.Code != 400 {
		t.Fatal("expired callback accepted")
	}
	if _, exists := app.pending[expiredState]; exists {
		t.Fatal("state not consumed")
	}
}

// TestCSRF はCSRFトークンとOriginのどちらが不正でも拒否されることを確認する。
func TestCSRF(t *testing.T) {
	app := &web{
		id:   "web-a",
		base: "http://127.0.0.1:9081",
		sessions: map[string]session{
			"sid": {CSRF: "correct", Until: time.Now().Add(time.Minute)},
		},
	}

	for _, item := range []struct {
		csrf, origin string
		expected     int
	}{
		{"wrong", app.base, 403},
		{"correct", "http://evil.example", 403},
		{"correct", app.base, 303},
	} {
		form := url.Values{"csrf": {item.csrf}, "operation": {"local-logout"}}
		request := httptest.NewRequest("POST", "/action", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", item.origin)
		request.AddCookie(&http.Cookie{Name: app.cookieName(), Value: "sid"})

		recorder := httptest.NewRecorder()
		app.action(recorder, request)
		if recorder.Code != item.expected {
			t.Fatalf("got %d want %d", recorder.Code, item.expected)
		}
	}
}

// TestResultStatus は結果の色分けが成功・拒否・エラーで分かれることを確認する。
func TestResultStatus(t *testing.T) {
	for _, item := range []struct {
		result, label, tone string
	}{
		{"http://api-a:8080/api/me\nHTTP 200\n{}", "HTTP 200", "ok"},
		{"http://api-b:8080/api/me\nHTTP 401\n", "HTTP 401", "denied"},
		{"Refresh HTTP 400; session may be revoked", "HTTP 400", "denied"},
		{"http://api-a:8080/api/documents\nHTTP 500\n", "HTTP 500", "error"},
		{"Refresh unavailable (network/IdP)", "エラー", "error"},
	} {
		label, tone := resultStatus(item.result)
		if label != item.label || tone != item.tone {
			t.Fatalf("%q: got %s/%s want %s/%s", item.result, label, tone, item.label, item.tone)
		}
	}
}

// TestBrowserPolicyHeaders は実ブラウザの動作に影響するヘッダーの値を固定する。
// Cookie jarの結合試験では、これらの不具合を検出できない。
func TestBrowserPolicyHeaders(t *testing.T) {
	// NewWebが起動時に読むDiscoveryだけを返す偽のIdP。
	var issuer string
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		JSON(writer, map[string]string{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/auth",
			"token_endpoint":         issuer + "/token",
			"jwks_uri":               issuer + "/jwks",
		})
	}))
	defer source.Close()
	issuer = source.URL

	handler, err := NewWeb(
		oidc.ClientContext(context.Background(), source.Client()),
		source.Client(),
		issuer,
		"web-a",
		"http://127.0.0.1:9081",
		"",
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))

	if got := recorder.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("browsers send Origin: null for form POST under %q", got)
	}
	if got := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(got, "form-action 'self' http://127.0.0.1:9080") {
		t.Fatalf("browsers block the SSO logout redirect under %q", got)
	}
}
