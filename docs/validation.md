# 検証記録

未計測の速度・工数・可用性を成果として主張しない。

## 最新の検証（2026-11-01 / Windows）

実ブラウザで見つかった2件の不具合（Referrer-PolicyとCSP）を修正した後のソースで、新規Volumeから確認した。

**環境**：Windows 11、Docker 29.9.2、Compose 5.6.1、Python 3.15.2（Windowsネイティブ）、Goテストは golang:1.27.3-bookworm コンテナ。ブラウザ操作はPlaywrightで自動化し、Chrome、Edge、Firefox 132を使用した。

| 検証 | 結果 | 方法 |
|---|---|---|
| Compose設定 | 成功 | docker compose config --quiet |
| Go race付きテスト、静的検査 | 成功 | Chapter 04のdocker runコマンド（TestBrowserPolicyHeadersを含む） |
| 新規VolumeからのDB・Realm初期化 | 成功 | setup.py、start.py、6サービス起動（healthcheck変更後の全結合試験を兼ねる） |
| 実ブラウザのSSO A→B | 成功 | 3ブラウザ。Bでパスワードフォームなし、sub一致 |
| 実ブラウザのAPI呼び出し、audience拒否 | 成功 | 3ブラウザ。me/admin=200、別API=401 |
| 実ブラウザの更新、SSOログアウト | 成功 | 3ブラウザ。Bのセッション残存、Bの更新=400 |
| 実ブラウザのローカルログアウト | 成功 | Chrome。Aへの再ログインでパスワード省略 |
| 実ブラウザのtokenダウンロード、文書一覧、bobの管理API | 成功 | Chrome。alice=id 1のみ、bob=id 2のみ、bob admin=403 |
| ブラウザ取得tokenでのseed/verify | 成功 | Chrome取得のtokenでseed.py、verify.py |
| browser_test.py、seed.py、verify.py | 成功 | すべてPASS |
| 第4章のcurl例 | 成功 | 200、401、404、400、WWW-Authenticate: Bearer |
| IdP停止、cold/warm cache、期限超過、復旧 | 成功 | failure_test.py --expiry |
| ユーザー無効化 | 成功 | 管理CLIで実施。新規ログイン拒否、既存JWT=200、有効化後にログイン可 |
| Cleanup | 成功 | down --volumes --remove-orphans後にコンテナとVolumeの不在を確認 |

公開ポートを9080 / 9081 / 9082 / 9001 / 9002へ変更した後に、新規Volumeから上表を再実行した。第4章のcurl例とユーザー無効化はポート変更前の結果で、変更後は再実行していない。

鍵ローテーションはポート変更後に確認した（管理CLIで優先度の高いrsa-generatedを追加し、新しいkidのtokenがAPI再起動なしで200）。

## 初回の検証（2026-11-01 / Linux）

Linux amd64、Docker Engine 29.1.3、Compose v5.6.1、Go 1.24.4公式コンテナ、Python 3.12で、Referrer-PolicyとCSPの修正、および公開ポートの変更（当時は8080 / 8081 / 8082 / 9001 / 9002）の前に確認した。

| 検証 | 結果 | 方法 |
|---|---|---|
| Compose設定 | 成功 | docker compose config --quiet |
| Goビルド | 成功 | Dockerfileのgo build、4サービスのイメージ構築 |
| Go race付きテスト | 成功 | go test -race ./... |
| Go静的検査 | 成功 | go vet ./... |
| Python構文 | 成功 | compileall |
| 新規VolumeからのDB・Realm初期化 | 成功 | postgres/keycloakと4つのGoサービス実起動 |
| SSO A→B | 成功 | HTTP Cookie jar、Aでのみパスワード送信 |
| API audience相互拒否 | 成功 | A→B、B→Aとも401、正しいAPIは200 |
| admin/user | 成功 | alice admin=200、bob admin=403 |
| 実DB所有者認可 | 成功 | bob→alice文書404、本人200、admin例外200、一覧本人限定 |
| 不正ID・不存在 | 成功 | 400 / 404 |
| 更新・ローカルログアウト・SSOログアウト | 成功 | browser_test.py、他アプリローカルsession残存とrefresh拒否も確認 |
| JWT異常claims・署名・alg | 成功 | テスト用RSA署名、iss/aud/exp/nbf/typ/sub、改ざん、RS512拒否 |
| JWKS鍵更新・取得失敗 | 成功 | httptestで既知鍵/未知鍵、warm/cold、公開鍵更新を確認 |
| IdP停止時の有効JWT | 成功 | 実Keycloak停止、warm cacheのA/Bとも200 |
| IdP停止時の新規ログイン・更新 | 成功 | 新規認可遷移が接続失敗、更新が利用不可 |
| IdP停止・API B再起動 | 成功 | cold B=401、warm A=200 |
| 実tokenの期限超過・復旧 | 成功 | failure_test.py --expiry、warm cacheでも期限超過401、Keycloak再起動 |
| nonce・state・CSRF | 成功 | 初回要求とPKCE対応、署名済みID Tokenのnonce不一致401、state再利用400、不正Origin/CSRF403 |
| 配布ZIP | 成功 | 別ディレクトリへ展開し、setupとCompose設定検証。実.env/tokenは除外し秘密値混入も検査 |
| Cleanup | 成功 | down --volumes --remove-orphans後に対象コンテナとDB Volumeの不在を確認 |

配布ZIPは廃止し、現在はリポジトリの `keycloak-go-sso/` を使う。

初期試験で検出した起動順序の競合とAccess Tokenのsub欠落は、start.pyの準備待ちとbasicスコープ追加で修正し、新規Volumeから再検証した。

その後PostgreSQLのhealthcheckを明示的なTCP接続に変更し、初期化途中の一時UNIX socketサーバーをreadyと判定しないようにした。この変更後の全結合試験は、上の最新の検証で実施した。

## 不具合の記録

どちらもCookie jarによる自動試験では検出できず、実ブラウザで発見した。browser_test.pyがOriginを固定値で送り、CSPも適用しないためである。

| 不具合 | 症状 | 修正 |
|---|---|---|
| `Referrer-Policy: no-referrer` | ブラウザが `Origin: null` を送り、/actionのPOSTがすべて403 | `same-origin` へ変更 |
| CSP `form-action 'self'` | Chrome/EdgeでSSOログアウトのリダイレクトがブロックされ、IdPセッションが残る | `http://127.0.0.1:9080` を許可 |

再発防止として、Goの `TestBrowserPolicyHeaders` でヘッダーの値を固定している。

## 未実施

- 管理画面（GUI）の操作：鍵ローテーション、ユーザー無効化、Realm設定の表示。いずれも操作そのものを画面では確認していない
- Realmのロール変更の反映
- WSL、macOS、Linux上の実ブラウザ、ARM環境（Linuxはプロトコル試験のみ、WindowsはWSLを経由せず実施）
- Chapter 03の手順を人間が手で行った場合の所要時間（180秒以内に収まるか）
- ブラウザのNetworkタブでの目視確認（Chapter 03 Step 4）
- 本番TLS・MFA・高可用性、性能/負荷/工数測定

Keycloak実鍵の試験手順はChapter 05にある。同等の公開鍵切り替え挙動の自動試験はGoテストで提供する。
