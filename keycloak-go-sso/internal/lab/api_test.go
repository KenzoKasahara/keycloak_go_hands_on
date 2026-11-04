package lab

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// signed はテスト用の鍵で署名したJWTを作る。claimsだけを変えた正しい署名のトークンで検証を確かめるため。
func signed(t *testing.T, key *rsa.PrivateKey, algorithm, kid string, claims map[string]any) string {
	t.Helper()

	header, _ := json.Marshal(map[string]string{"alg": algorithm, "kid": kid})
	payload, _ := json.Marshal(claims)
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)

	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// TestJWTAndJWKS は偽のJWKSサーバーを使い、claims検証と鍵キャッシュの挙動を確認する。
func TestJWTAndJWKS(t *testing.T) {
	first, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	second, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	// 偽のJWKSサーバー。停止と鍵の切り替えをテストから操作できる。
	var rotated atomic.Bool
	var unavailable atomic.Bool
	var calls atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if unavailable.Load() {
			http.Error(writer, "down", 503)
			return
		}

		key, kid := first, "first"
		if rotated.Load() {
			key, kid = second, "second"
		}

		JSON(writer, map[string]any{"keys": []any{map[string]string{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": kid,
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	defer source.Close()

	client := &http.Client{Timeout: time.Second}
	ctx := oidc.ClientContext(context.Background(), client)

	// newHandler を呼ぶたびに、鍵キャッシュが空のハンドラーができる。
	newHandler := func() http.Handler {
		verifier := oidc.NewVerifier(
			"https://issuer.example",
			oidc.NewRemoteKeySet(ctx, source.URL),
			&oidc.Config{ClientID: "api-a", SupportedSigningAlgs: []string{"RS256"}},
		)
		return Authenticate(verifier, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(200)
		}))
	}
	handler := newHandler()

	valid := func() map[string]any {
		return map[string]any{
			"iss": "https://issuer.example",
			"aud": "api-a",
			"sub": "bob",
			"typ": "Bearer",
			"exp": time.Now().Add(time.Minute).Unix(),
		}
	}

	invoke := func(handler http.Handler, token string) int {
		request := httptest.NewRequest("GET", "/api/me", nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}

	// 正常な1件を基準に、1項目だけ不正にしたトークンがそれぞれ401になることを確かめる。
	cases := []struct {
		name, field string
		value       any
		algorithm   string
		expected    int
	}{
		{"valid", "", nil, "RS256", 200},
		{"issuer", "iss", "https://wrong.example", "RS256", 401},
		{"audience", "aud", "api-b", "RS256", 401},
		{"expired", "exp", time.Now().Add(-time.Minute).Unix(), "RS256", 401},
		{"not-before", "nbf", time.Now().Add(time.Hour).Unix(), "RS256", 401},
		{"ID-token", "typ", "ID", "RS256", 401},
		{"subject", "sub", "", "RS256", 401},
		{"algorithm", "", nil, "RS512", 401},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			claims := valid()
			if item.field != "" {
				claims[item.field] = item.value
			}
			if status := invoke(handler, signed(t, first, item.algorithm, "first", claims)); status != item.expected {
				t.Fatalf("got %d want %d", status, item.expected)
			}
		})
	}

	if invoke(handler, "") != 401 {
		t.Fatal("missing token accepted")
	}

	// 署名後にpayloadを書き換えたトークンは署名検証で拒否される。
	token := signed(t, first, "RS256", "first", valid())
	parts := strings.Split(token, ".")
	altered := valid()
	altered["sub"] = "mallory"
	payload, _ := json.Marshal(altered)
	parts[1] = base64.RawURLEncoding.EncodeToString(payload)
	if invoke(handler, strings.Join(parts, ".")) != 401 {
		t.Fatal("tampered token accepted")
	}

	// warm cache：JWKSが停止しても、キャッシュ済みの鍵で検証でき、JWKSへのアクセスも発生しない。
	unavailable.Store(true)
	previous := calls.Load()
	if invoke(handler, token) != 200 || calls.Load() != previous {
		t.Fatal("warm cache failed")
	}

	// cold cache：キャッシュが空のハンドラーは鍵を取得できず拒否する。
	if invoke(newHandler(), token) != 401 {
		t.Fatal("cold cache allowed unavailable JWKS")
	}

	// 鍵ローテーション：未知のkidは、JWKSが復旧して再取得できるまで受け入れない。
	rotated.Store(true)
	next := signed(t, second, "RS256", "second", valid())
	if invoke(handler, next) != 401 {
		t.Fatal("unknown key allowed while unavailable")
	}

	unavailable.Store(false)
	if invoke(handler, next) != 200 {
		t.Fatal("rotation refresh failed")
	}
}

// fakeDocuments は認可条件の受け渡しだけを確認するためのRepository。id=1の所有者はalice。
type fakeDocuments struct{}

func (fakeDocuments) List(ctx context.Context, owner string) ([]Document, error) {
	return []Document{{ID: 2, Owner: owner}}, nil
}

func (fakeDocuments) Get(ctx context.Context, id int64, owner string, admin bool) (Document, error) {
	if id == 1 && (owner == "alice" || admin) {
		return Document{ID: 1, Owner: "alice"}, nil
	}
	return Document{}, sql.ErrNoRows
}

// TestAuthorization はRBACと所有者認可のHTTPステータスを確認する。
func TestAuthorization(t *testing.T) {
	cases := []struct {
		name, path, subject string
		admin               bool
		expected            int
	}{
		{"admin", "/api/admin", "alice", true, 200},
		{"user-admin-denied", "/api/admin", "bob", false, 403},
		{"owner", "/api/documents/1", "alice", false, 200},
		{"IDOR", "/api/documents/1", "bob", false, 404},
		{"admin-other-owner", "/api/documents/1", "carol", true, 200},
		{"missing", "/api/documents/999", "alice", true, 404},
		{"invalid", "/api/documents/1%27OR%271", "bob", false, 400},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			identity := Identity{Subject: item.subject}
			if item.admin {
				identity.Realm.Roles = []string{"admin"}
			}

			request := httptest.NewRequest("GET", item.path, nil)
			request = request.WithContext(context.WithValue(request.Context(), identityKey{}, identity))
			recorder := httptest.NewRecorder()
			Protected("api-b", fakeDocuments{}).ServeHTTP(recorder, request)

			if recorder.Code != item.expected {
				t.Fatalf("got %d want %d", recorder.Code, item.expected)
			}
		})
	}
}
