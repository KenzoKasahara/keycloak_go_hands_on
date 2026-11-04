# Chapter 01: 準備と仮説

## この章の目的

なぜ2つのアプリで検証するのかと、各構成要素の責任を把握する。そのうえで、認証・認可・可用性を別々に評価するための観察表を用意する。

## 現在地

**準備と仮説** → 環境構築 → SSOと認可の観察 → 自動テストと機能検証 → 障害試験 → 振り返り → 片付け

## 完了条件

- [ ] 前提条件を満たし、必要なポートが空いている
- [ ] `keycloak-go-sso/` へ移動した
- [ ] 観察表に予想を記入し、SSOとtoken共有、ユーザー権限とレコード権限を別の仮説として言語化できる

## 背景

単一APIだけでは外部IdPの価値が見えにくい。2つの独立したWebアプリを設け、共通ユーザーでのSSO、個別audience、アプリごとの認可、共通障害点を観察する。

認証基盤を共有すると、パスワードポリシー、MFA、ユーザー管理、外部IdP連携を各アプリに再実装する必要が減る。ただし本教材でMFAや外部IdP自体を構築するわけではない。

## 構成要素と責任

全体図は[README](../README.md#architecture)を参照。

| 構成 | 技術 | 責任 |
|---|---|---|
| web-a / web-b | Go net/http、html/template、oauth2 | 独立したPublic OIDCクライアント、Code交換、セッション、PKCE/state/nonce |
| api-a | Go net/http、go-oidc | audience=api-a、署名/claims検証、RBAC |
| api-b | Go、database/sql、pgx | audience=api-b、RBAC、所有者SQLフィルター |
| keycloak | 26.7.0 start-dev | Realm、共通ユーザー、SSO、トークン発行、JWKS |
| postgres | 17.5 | DB keycloak/appとログインRoleの論理分離 |
| テスト | Go testing/httptest、Python標準ライブラリ | 署名済み不正claims、JWKS障害、HTTP結合試験 |

WebはフレームワークやブラウザJavaScriptを使わないサーバー描画方式。認証コード交換主体は各Webサーバーであり、ブラウザはKeycloakへの遷移とコールバックを行う。Public ClientとしてPKCEを必須にし、client secretを使わない。実際のサーバーアプリ本番ではConfidential Client + PKCEも検討する。

### issuerと接続先

ブラウザのissuerは `http://127.0.0.1:9080/realms/sso-lab` に固定する。localhostへ読み替えない。

GoのHTTP transportは、このURLの接続先 `127.0.0.1:9080` だけを `keycloak:8080` に置き換える。URL・Hostヘッダー・期待issuerは保持する。issuerチェックを回避する仕掛けではない。任意のホストを書き換えず、教材の固定接続先だけを扱う。実装は [cmd/lab/main.go](../keycloak-go-sso/cmd/lab/main.go)。

APIはDiscoveryに起動依存しないよう、Keycloakで既知のJWKS URLを明示する。WebはDiscoveryで認可・token endpointを取得する。これによりIdP停止中にAPIを再起動して「空の鍵キャッシュ」を作る試験ができる。API起動成功は鍵取得成功を意味しない。

## 前提条件

| 項目 | 内容 |
|---|---|
| Docker | Docker Engine / Docker DesktopとCompose v2以降 |
| Python | 3.10以降 |
| その他ツール | ブラウザ、curl、初回依存取得用インターネット接続 |
| シェル | Linux/WSLのbashを基準とする。WindowsではWSLから実行する |
| Go | コンテナ内で利用するためホストへの導入不要 |
| リソース | 4GB以上の空きメモリと数GBのディスク（保証値・性能測定値ではない） |
| ポート | 9080 / 9081 / 9082 / 9001 / 9002 を空ける（コンテナ内部は各サービスとも8080） |

**ブラウザプロファイル**：SSOは同じブラウザプロファイルで試す。bobの試験には別プロファイルまたはプライベートウィンドウを使用する。プライベートウィンドウ同士が同じCookie領域を共有するブラウザもあるので、aliceとbobを混在させない。

**セキュリティ上の範囲**：全公開ポートは127.0.0.1限定。PostgreSQLはホストへ公開しない。HTTPとSecure=falseのCookie、start-dev、Realm sslRequired=noneはローカル専用。イメージは教材の固定版で、最新安全版や本番推奨を保証しない。

## リポジトリ構成

```text
.
├── README.md                 ハンズオンの入口
├── docs/                     各章と検証記録
└── keycloak-go-sso/          実装（コマンドはここで実行する）
    ├── compose.yaml
    ├── Dockerfile
    ├── .env.example
    ├── .dockerignore / .gitignore / .gitattributes
    ├── go.mod / go.sum
    ├── cmd/lab/main.go
    ├── internal/lab/
    │   ├── api.go / api_test.go
    │   └── web.go / web_test.go
    ├── db/init.sh
    └── scripts/
        ├── setup.py
        ├── start.py
        ├── seed.py
        ├── verify.py
        ├── failure_test.py
        └── browser_test.py
```

| ファイル | 役割 | 主に使う章 |
|---|---|---|
| [compose.yaml](../keycloak-go-sso/compose.yaml) / [Dockerfile](../keycloak-go-sso/Dockerfile) | 6サービスの定義、Goイメージのビルド | 02 |
| [.env.example](../keycloak-go-sso/.env.example) | 環境変数名の仕様 | 02 |
| [db/init.sh](../keycloak-go-sso/db/init.sh) | DB・Role・documentsテーブルの初期化 | 02 |
| [scripts/setup.py](../keycloak-go-sso/scripts/setup.py) | `.env`とRealm JSONの生成 | 02 |
| [scripts/start.py](../keycloak-go-sso/scripts/start.py) | Keycloakの準備を待ってWeb/APIを起動 | 02、05 |
| [cmd/lab/main.go](../keycloak-go-sso/cmd/lab/main.go) | 接続先の置き換え、api/webモードの起動 | 02 |
| [internal/lab/web.go](../keycloak-go-sso/internal/lab/web.go) | OIDCクライアント、セッション、CSRF、画面 | 03 |
| [internal/lab/api.go](../keycloak-go-sso/internal/lab/api.go) | JWT検証Middleware、RBAC、所有者認可 | 03 |
| [scripts/seed.py](../keycloak-go-sso/scripts/seed.py) | 検証済みsubを所有者とする文書の投入 | 03 |
| [internal/lab/api_test.go](../keycloak-go-sso/internal/lab/api_test.go) / [web_test.go](../keycloak-go-sso/internal/lab/web_test.go) | 不正claims、JWKS障害、PKCE/state/nonce、CSRF、ブラウザ向けヘッダー | 04 |
| [scripts/verify.py](../keycloak-go-sso/scripts/verify.py) | 実サービスへのHTTP結合試験 | 04 |
| [scripts/browser_test.py](../keycloak-go-sso/scripts/browser_test.py) | Cookie jarによるSSO・更新・ログアウトの試験 | 04 |
| [scripts/failure_test.py](../keycloak-go-sso/scripts/failure_test.py) | IdP停止とキャッシュの障害試験 | 05 |

`.env`と`generated/`はsetupで生成し、Git・Docker build contextには含めない。Realm JSONには `${ALICE_PASSWORD}` のようなプレースホルダーだけを出力し、Keycloakが自身の環境変数から展開する。実パスワードは.envから注入する。Docker管理者や同一ユーザーからの秘匿を保証する仕組みではない。

`.gitattributes` は改行コードをLFに固定する。Windowsでチェックアウトしても、コンテナ内で実行する `db/init.sh` が壊れないようにするためである。

## Step 1. 仮説と観察表を準備する

### やること

リポジトリを取得して実装ディレクトリへ移動する。次の表を手元にコピーし、予想と観察を別々に記入する。

### 実行

```bash
git clone https://github.com/KenzoKasahara/keycloak_go_hands_on.git
cd keycloak_go_hands_on/keycloak-go-sso
```

| 操作 | 予想 | 実際の観察 | 説明 |
|---|---|---|---|
| A→Bログイン | パスワード省略 | | |
| A token→B API | 401 | | |
| bob→管理API | 403 | | |
| bob→alice文書 | 404 | | |
| IdP停止・warm cache | 期限内200 | | |
| IdP停止・cold cache | 401 | | |
| IdP停止・更新 | 失敗 | | |

### 期待結果

SSOとtoken共有、ユーザー権限とレコード権限を別の仮説として言語化できる。この段階ではまだサービスを作らない。

### なぜ行うのか

HTTP 200/401だけでなく、どの処理がどこで行われたかまで確認するため。

---

次へ：[Chapter 02: 環境構築](./02-setup.md)
