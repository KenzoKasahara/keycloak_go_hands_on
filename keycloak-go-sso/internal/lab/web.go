// web.go はWeb側（OIDCクライアント、セッション、CSRF対策、画面）を担当する。
package lab

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// randomValue はstate・nonce・セッションIDなどに使う推測不能な値を作る。
func randomValue() string {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}

// pending はログイン開始からコールバックまで保持する、認可要求ごとの値。
type pending struct {
	State, Nonce, Verifier string
	Until                  time.Time
}

// session はログイン後のサーバー側セッション。トークンはブラウザへ渡さずここに持つ。
type session struct {
	CSRF, IDToken, Subject, Nonce string
	Token                         *oauth2.Token
	Until                         time.Time
}

// web はアプリ1つ分の状態。セッションはメモリ上にあり、再起動で消える。
type web struct {
	mutex                        sync.Mutex
	pending                      map[string]pending
	sessions                     map[string]session
	config                       oauth2.Config
	verifier                     *oidc.IDTokenVerifier
	client                       *http.Client
	issuer, id, base, api, other string
}

// page は画面全体のテンプレート。html/templateが出力をエスケープする。
// CSPでスクリプトを許可していないので、画面はHTMLとインラインCSSだけで作る。
var page = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="ja"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.App}} SSO Lab</title>
<style>
:root{color-scheme:light dark;--bg:#f4f6f9;--surface:#fff;--text:#1c2430;--muted:#5d6878;--line:#d9dfe7;--code:#f1f3f6;--accent:#2559c7;--on-accent:#fff;--ok:#17703f;--ok-bg:#e3f4ea;--denied:#8a5a00;--denied-bg:#fbf0d6;--error:#b3261e;--error-bg:#fbe6e4}
.web-b{--accent:#0f766e}
@media (prefers-color-scheme:dark){
:root{--bg:#12161c;--surface:#1b2129;--text:#e7ebf0;--muted:#a0abb9;--line:#333c48;--code:#12161c;--accent:#86aefc;--on-accent:#10182a;--ok:#7fd8a4;--ok-bg:#16321f;--denied:#f0c565;--denied-bg:#37290c;--error:#f5a39d;--error-bg:#3d1a18}
.web-b{--accent:#5fd0c5}
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);font:16px/1.6 system-ui,"Segoe UI","Hiragino Sans","Yu Gothic UI",sans-serif;border-top:4px solid var(--accent)}
.wrap{max-width:860px;margin:0 auto;padding:24px 16px 40px}
header{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:12px;margin-bottom:20px}
h1{margin:0;font-size:1.5rem;line-height:1.3}
h2{margin:0 0 4px;font-size:1rem}
nav{display:flex;gap:4px;padding:4px;background:var(--surface);border:1px solid var(--line);border-radius:8px}
nav a{padding:4px 14px;border-radius:5px;color:var(--muted);text-decoration:none}
nav a:hover{color:var(--text)}
nav a[aria-current]{background:var(--accent);color:var(--on-accent);font-weight:600}
.card{background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:18px 20px;margin-bottom:16px}
.hint{margin:0 0 12px;color:var(--muted);font-size:.875rem}
dl{display:grid;grid-template-columns:max-content 1fr;gap:4px 16px;margin:0}
dt{color:var(--muted);font-size:.875rem}
dd{margin:0;font-family:ui-monospace,Consolas,monospace;font-size:.875rem;overflow-wrap:anywhere}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(340px,1fr));gap:16px;margin-bottom:16px}
.grid .card{margin:0}
.controls{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
.controls+.controls{margin-top:8px}
button,.button{font:inherit;font-size:.875rem;padding:7px 14px;border-radius:6px;border:1px solid var(--line);background:var(--surface);color:var(--text);cursor:pointer;text-decoration:none;display:inline-block}
button:hover{border-color:var(--accent);color:var(--accent)}
button:focus-visible,a:focus-visible,input:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
.button{background:var(--accent);border-color:var(--accent);color:var(--on-accent);font-size:1rem;font-weight:600;padding:9px 20px}
.danger{color:var(--error)}
button.danger:hover{border-color:var(--error);color:var(--error)}
label{font-size:.875rem;color:var(--muted)}
input[name=document]{font:inherit;font-size:.875rem;width:5em;padding:6px 8px;margin-left:6px;border:1px solid var(--line);border-radius:6px;background:var(--surface);color:var(--text)}
.result-head{display:flex;align-items:center;gap:10px;margin-bottom:10px}
.result-head h2{margin:0}
.badge{font-size:.8125rem;font-weight:600;padding:1px 10px;border-radius:999px}
.ok{color:var(--ok);background:var(--ok-bg)}
.denied{color:var(--denied);background:var(--denied-bg)}
.error{color:var(--error);background:var(--error-bg)}
pre{margin:0;padding:12px 14px;background:var(--code);border-radius:6px;font:.8125rem/1.55 ui-monospace,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere;max-height:420px;overflow:auto}
footer{color:var(--muted);font-size:.8125rem}
@media (max-width:480px){.grid{grid-template-columns:1fr}}
</style></head>
<body class="{{.App}}"><div class="wrap">
<header><h1>{{.App}} SSO Lab</h1>
<nav><a href="http://127.0.0.1:9081/"{{if eq .App "web-a"}} aria-current="page"{{end}}>App A</a><a href="http://127.0.0.1:9082/"{{if eq .App "web-b"}} aria-current="page"{{end}}>App B</a></nav></header>
{{if .Subject}}<section class="card"><h2>ログイン中</h2><p class="hint">このアプリのセッションはサーバーのメモリにあり、トークンはブラウザへ渡していません。</p>
<dl><dt>sub:</dt><dd>{{.Subject}}</dd><dt>Access Token expires:</dt><dd>{{.Expiry}}</dd></dl></section>
{{if .Result}}<section class="card"><div class="result-head"><h2>結果</h2><span class="badge {{.Tone}}">{{.Status}}</span></div><pre>{{.Result}}</pre></section>{{end}}
<div class="grid">
<section class="card"><h2>APIを呼ぶ</h2><p class="hint">Webサーバーがトークンを付けて、このアプリ用のAPIを呼びます。</p>
<form method="post" action="/action" class="controls"><input type="hidden" name="csrf" value="{{.CSRF}}">
<button name="operation" value="me">API /me</button><button name="operation" value="admin">API /admin</button></form></section>
<section class="card"><h2>文書を取得する（App B）</h2><p class="hint">一覧は本人の文書だけ、個別取得はadminのみ他者の文書も取得できます。</p>
<form method="post" action="/action" class="controls"><input type="hidden" name="csrf" value="{{.CSRF}}">
<button name="operation" value="documents">API /documents (B)</button></form>
<form method="post" action="/action" class="controls"><input type="hidden" name="csrf" value="{{.CSRF}}">
<label>文書ID<input name="document" value="1" size="5"></label><button name="operation" value="document">文書ID取得 (B)</button></form></section>
<section class="card"><h2>トークンを試す</h2><p class="hint">audienceの分離と手動更新を観察します。ダウンロードしたトークンは外部へ共有しないでください。</p>
<form method="post" action="/action" class="controls"><input type="hidden" name="csrf" value="{{.CSRF}}">
<button name="operation" value="cross">別APIへ同じトークン</button><button name="operation" value="refresh">トークン更新</button><button name="operation" value="export">学習用Access Tokenダウンロード</button></form></section>
<section class="card"><h2>ログアウトする</h2><p class="hint">ローカルログアウトではKeycloakのSSOセッションが残ります。SSOログアウトでは終了します。</p>
<form method="post" action="/action" class="controls"><input type="hidden" name="csrf" value="{{.CSRF}}">
<button name="operation" value="local-logout">このアプリだけログアウト</button><button class="danger" name="operation" value="sso-logout">Keycloak SSOログアウト</button></form></section>
</div>
{{else}}<section class="card"><h2>未ログイン</h2><p class="hint">Keycloakで認証します。もう一方のアプリでログイン済みなら、パスワード入力なしで戻ってきます。</p>
<a class="button" href="/login">Keycloakでログイン</a></section>
{{if .Result}}<section class="card"><div class="result-head"><h2>結果</h2><span class="badge {{.Tone}}">{{.Status}}</span></div><pre>{{.Result}}</pre></section>{{end}}
{{end}}<footer>HTTP・メモリセッション・トークン書き出しはローカル教材専用。</footer>
</div></body></html>`))

// statusPattern は結果の文字列からHTTPステータスを取り出す。
var statusPattern = regexp.MustCompile(`HTTP (\d{3})`)

// resultStatus は結果の見出しに出すラベルと色分けを返す。4xxは教材で観察する拒否なのでエラーと分ける。
func resultStatus(result string) (string, string) {
	match := statusPattern.FindStringSubmatch(result)
	if match == nil {
		return "エラー", "error"
	}
	switch match[1][0] {
	case '2':
		return match[0], "ok"
	case '4':
		return match[0], "denied"
	}
	return match[0], "error"
}

// cookieName はアプリごとのCookie名を返す。Cookieはポートで分離されないので名前で分ける。
func (app *web) cookieName() string { return "sso_lab_" + app.id }

// cookie はセッションCookieを設定する。ageに負の値を渡すと削除になる。
func (app *web) cookie(writer http.ResponseWriter, value string, age int) {
	http.SetCookie(writer, &http.Cookie{
		Name:     app.cookieName(),
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   age,
	})
}

// current はCookieから有効なセッションを探す。期限切れならここで削除する。
func (app *web) current(request *http.Request) (string, session, bool) {
	cookie, err := request.Cookie(app.cookieName())
	if err != nil {
		return "", session{}, false
	}

	app.mutex.Lock()
	defer app.mutex.Unlock()

	value, ok := app.sessions[cookie.Value]
	if ok && time.Now().After(value.Until) {
		delete(app.sessions, cookie.Value)
		ok = false
	}
	return cookie.Value, value, ok
}

// render はセッションの状態と結果を画面へ描画する。
func (app *web) render(writer http.ResponseWriter, request *http.Request, result string) {
	_, value, _ := app.current(request)

	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")

	expiry := ""
	if value.Token != nil {
		expiry = value.Token.Expiry.Format(time.RFC3339)
	}

	status, tone := resultStatus(result)

	page.Execute(writer, map[string]string{
		"App":     app.id,
		"Subject": value.Subject,
		"CSRF":    value.CSRF,
		"Expiry":  expiry,
		"Result":  result,
		"Status":  status,
		"Tone":    tone,
	})
}

// login は認可要求の値を保存し、ブラウザをKeycloakへリダイレクトする。
func (app *web) login(writer http.ResponseWriter, request *http.Request) {
	state := randomValue()
	value := pending{
		State:    state,
		Nonce:    randomValue(),
		Verifier: oauth2.GenerateVerifier(),
		Until:    time.Now().Add(3 * time.Minute),
	}

	app.mutex.Lock()
	for key, item := range app.pending {
		if time.Now().After(item.Until) {
			delete(app.pending, key)
		}
	}

	// 未認証で呼べる処理なので、メモリを使い切られないよう件数を制限する。
	if len(app.pending) >= 100 {
		app.mutex.Unlock()
		http.Error(writer, "too many logins", 429)
		return
	}
	app.pending[state] = value
	app.mutex.Unlock()

	// stateをこのブラウザに結び付けるCookie。コールバックで照合する。
	http.SetCookie(writer, &http.Cookie{
		Name:     app.cookieName() + "_flow",
		Value:    state,
		Path:     "/callback",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   180,
	})

	address := app.config.AuthCodeURL(state, oidc.Nonce(value.Nonce), oauth2.S256ChallengeOption(value.Verifier))
	http.Redirect(writer, request, address, http.StatusFound)
}

// callback は認可応答を検証してトークンを取得し、ログインセッションを作る。
func (app *web) callback(writer http.ResponseWriter, request *http.Request) {
	// stateがCookieと一致しない応答は、別のブラウザで始まった可能性があるので拒否する。
	state := request.URL.Query().Get("state")
	cookie, err := request.Cookie(app.cookieName() + "_flow")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		http.Error(writer, "invalid state", 400)
		return
	}

	// 成否にかかわらず削除し、同じstateを再利用させない。
	app.mutex.Lock()
	value, ok := app.pending[state]
	delete(app.pending, state)
	app.mutex.Unlock()

	http.SetCookie(writer, &http.Cookie{
		Name:     app.cookieName() + "_flow",
		Path:     "/callback",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	if !ok || time.Now().After(value.Until) || request.URL.Query().Get("code") == "" {
		http.Error(writer, "expired or rejected login", 400)
		return
	}

	ctx := oidc.ClientContext(request.Context(), app.client)

	// 認可コードの交換。PKCEのverifierがなければKeycloakに拒否される。
	token, err := app.config.Exchange(ctx, request.URL.Query().Get("code"), oauth2.VerifierOption(value.Verifier))
	if err != nil {
		http.Error(writer, "code exchange failed", 502)
		return
	}

	raw, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(writer, "missing ID Token", 502)
		return
	}

	// ID Tokenの検証に加えて、nonceでこの認可要求への応答であることを確認する。
	idToken, err := app.verifier.Verify(ctx, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(value.Nonce)) != 1 {
		http.Error(writer, "invalid ID Token or nonce", 401)
		return
	}

	// ログインの度に新しいセッションIDを発行する（セッション固定攻撃の対策）。
	key := randomValue()
	_, _, existed := app.current(request)

	app.mutex.Lock()
	for previous, item := range app.sessions {
		if time.Now().After(item.Until) {
			delete(app.sessions, previous)
		}
	}
	if existed {
		if previous, cookieErr := request.Cookie(app.cookieName()); cookieErr == nil {
			delete(app.sessions, previous.Value)
		}
	}
	if len(app.sessions) >= 100 {
		app.mutex.Unlock()
		http.Error(writer, "too many sessions", 429)
		return
	}
	app.sessions[key] = session{
		CSRF:    randomValue(),
		IDToken: raw,
		Subject: idToken.Subject,
		Nonce:   value.Nonce,
		Token:   token,
		Until:   time.Now().Add(30 * time.Minute),
	}
	app.mutex.Unlock()

	app.cookie(writer, key, 1800)
	http.Redirect(writer, request, "/", 303)
}

// action は画面のボタン操作を処理する。状態を変える操作はすべてこのPOSTで受ける。
func (app *web) action(writer http.ResponseWriter, request *http.Request) {
	key, value, ok := app.current(request)

	request.Body = http.MaxBytesReader(writer, request.Body, 4096)
	if request.ParseForm() != nil {
		http.Error(writer, "invalid form", 400)
		return
	}

	// CSRF対策として、CSRFトークンとOriginの両方を照合する。
	if !ok ||
		subtle.ConstantTimeCompare([]byte(request.FormValue("csrf")), []byte(value.CSRF)) != 1 ||
		request.Header.Get("Origin") != app.base {
		http.Error(writer, "invalid session, origin or CSRF", 403)
		return
	}

	switch request.FormValue("operation") {
	case "local-logout", "sso-logout":
		// どちらのログアウトでも、まずこのアプリのセッションを消す。
		app.mutex.Lock()
		delete(app.sessions, key)
		app.mutex.Unlock()
		app.cookie(writer, "", -1)

		if request.FormValue("operation") == "local-logout" {
			http.Redirect(writer, request, "/", 303)
			return
		}

		// RP-Initiated Logout：KeycloakへリダイレクトしてIdPセッションを終了する。
		query := url.Values{
			"id_token_hint":            {value.IDToken},
			"post_logout_redirect_uri": {app.base + "/"},
			"client_id":                {app.id},
		}
		http.Redirect(writer, request, app.issuer+"/protocol/openid-connect/logout?"+query.Encode(), 303)
		return

	case "export":
		// 学習用にAccess Tokenをダウンロードさせる。本番では作ってはいけない機能。
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Content-Disposition", `attachment; filename="`+app.id+`.token"`)
		io.WriteString(writer, value.Token.AccessToken)
		return

	case "refresh":
		// 障害時の失敗を観察できるよう、自動更新はせずボタンで更新する。
		if value.Token.RefreshToken == "" {
			app.render(writer, request, "No refresh token")
			return
		}

		form := url.Values{
			"grant_type":    {"refresh_token"},
			"client_id":     {app.id},
			"refresh_token": {value.Token.RefreshToken},
		}
		refreshRequest, _ := http.NewRequestWithContext(
			request.Context(),
			"POST",
			app.config.Endpoint.TokenURL,
			strings.NewReader(form.Encode()),
		)
		refreshRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		response, err := app.client.Do(refreshRequest)
		if err != nil {
			app.render(writer, request, "Refresh unavailable (network/IdP)")
			return
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			app.render(writer, request, fmt.Sprintf("Refresh HTTP %d; session may be revoked", response.StatusCode))
			return
		}

		var tokens struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
			ID      string `json:"id_token"`
			Expires int64  `json:"expires_in"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&tokens) != nil || tokens.Access == "" {
			app.render(writer, request, "Invalid token response")
			return
		}

		// 更新時のID Tokenも署名を検証し、同じユーザーであることを確認する。
		if tokens.ID != "" {
			verified, err := app.verifier.Verify(oidc.ClientContext(request.Context(), app.client), tokens.ID)
			if err != nil ||
				verified.Subject != value.Subject ||
				(verified.Nonce != "" && subtle.ConstantTimeCompare([]byte(verified.Nonce), []byte(value.Nonce)) != 1) {
				app.render(writer, request, "Invalid refreshed ID Token")
				return
			}
			value.IDToken = tokens.ID
		}

		value.Token = &oauth2.Token{
			AccessToken:  tokens.Access,
			TokenType:    "Bearer",
			RefreshToken: value.Token.RefreshToken,
			Expiry:       time.Now().Add(time.Duration(tokens.Expires) * time.Second),
		}
		if tokens.Refresh != "" {
			value.Token.RefreshToken = tokens.Refresh
		}

		app.mutex.Lock()
		if _, active := app.sessions[key]; active {
			app.sessions[key] = value
		}
		app.mutex.Unlock()

		app.render(writer, request, "Refresh HTTP 200")
		return
	}

	// ここから下はAPI呼び出し。ブラウザではなくWebサーバーがトークンを付けて呼ぶ。
	address := app.api
	path := "/api/me"

	switch request.FormValue("operation") {
	case "me":
	case "admin":
		path = "/api/admin"
	case "documents":
		path = "/api/documents"
	case "document":
		path = "/api/documents/" + url.PathEscape(request.FormValue("document"))
	case "cross":
		// audience分離の確認用に、あえて別のAPIへ同じトークンを送る。
		address = app.other
	default:
		http.Error(writer, "unknown action", 400)
		return
	}

	call, _ := http.NewRequestWithContext(request.Context(), "GET", address+path, nil)
	call.Header.Set("Authorization", "Bearer "+value.Token.AccessToken)

	response, err := app.client.Do(call)
	if err != nil {
		app.render(writer, request, "API unavailable")
		return
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(response.Body, 65536))

	// JSONは読みやすいように整形する。JSONでなければそのまま表示する。
	var indented bytes.Buffer
	if json.Indent(&indented, body, "", "  ") == nil {
		body = indented.Bytes()
	}
	app.render(writer, request, fmt.Sprintf("%s%s\nHTTP %d\n%s", address, path, response.StatusCode, body))
}

// NewWeb はDiscoveryでKeycloakのエンドポイントを取得し、Webのハンドラーを組み立てる。
// Keycloakが準備できていないと失敗するので、start.pyは準備を待ってから起動する。
func NewWeb(ctx context.Context, client *http.Client, issuer, id, base, api, other string) (http.Handler, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}

	app := &web{
		pending:  map[string]pending{},
		sessions: map[string]session{},
		client:   client,
		issuer:   issuer,
		id:       id,
		base:     base,
		api:      api,
		other:    other,
		config: oauth2.Config{
			ClientID:    id,
			RedirectURL: base + "/callback",
			Scopes:      []string{oidc.ScopeOpenID, "profile", "email"},
			Endpoint:    provider.Endpoint(),
		},
		verifier: provider.Verifier(&oidc.Config{
			ClientID:             id,
			SupportedSigningAlgs: []string{"RS256"},
		}),
	}

	router := http.NewServeMux()
	router.HandleFunc("GET /{$}", func(writer http.ResponseWriter, request *http.Request) {
		app.render(writer, request, "")
	})
	router.HandleFunc("GET /login", app.login)
	router.HandleFunc("GET /callback", app.callback)
	router.HandleFunc("POST /action", app.action)

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")

		// no-referrerにするとブラウザがフォームPOSTで Origin: null を送り、Origin照合に失敗する。
		writer.Header().Set("Referrer-Policy", "same-origin")

		// form-actionはPOST後のリダイレクト先にも適用されるので、SSOログアウトの遷移先Keycloakを許可する。
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'none'; form-action 'self' http://127.0.0.1:9080")

		router.ServeHTTP(writer, request)
	}), nil
}
