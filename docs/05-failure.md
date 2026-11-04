# Chapter 05: 障害試験

## この章の目的

ログイン・更新・オフラインJWT検証の障害境界を確認する。また、既知鍵のキャッシュと未知kidによる再取得を観察する。

## 現在地

準備と仮説 → 環境構築 → SSOと認可の観察 → 自動テストと機能検証 → **障害試験** → 振り返り → 片付け

## 完了条件

- [ ] IdP停止中の新規ログイン、更新、warm cache、cold cacheの結果を区別して説明できる
- [ ] Keycloakを復旧し、API Bが200へ戻ることを確認した
- [ ] 鍵ローテーション後の新しいkidのtokenが、API再起動なしで200になる

## 何が止まり、何が動くか

```mermaid
flowchart LR
  KC[Keycloak 停止]:::down
  KC -.->|止まる| L[新規ログイン]
  KC -.->|止まる| R[トークン更新]
  KC -.->|鍵を取得できない| C[cold cacheのAPI: 401]
  W[warm cacheのAPI] -->|Keycloakへ問い合わせず検証| OK[期限内のJWT: 200]
  classDef down stroke-dasharray: 5 5
```

## Step 7. Keycloak停止と鍵キャッシュを分けて観察する

### やること

Keycloakだけ停止する。DB、Web、APIは動作を継続する。

A/Bを再ログインまたは更新して、直前発行のtokenを保存する。A/Bのmeを1回ずつ200にし、APIの鍵キャッシュを温める。ログアウト試験後の古いtokenを使い回さない。

### 実行

```bash
curl -fsS -H "Authorization: Bearer $(cat generated/alice-a.token)" http://127.0.0.1:9001/api/me
curl -fsS -H "Authorization: Bearer $(cat generated/alice-b.token)" http://127.0.0.1:9002/api/me
docker compose stop keycloak
curl -i -H "Authorization: Bearer $(cat generated/alice-a.token)" http://127.0.0.1:9001/api/me
curl -i -H "Authorization: Bearer $(cat generated/alice-b.token)" http://127.0.0.1:9002/api/me
```

続けてブラウザで2点を確認する。

- Webの「トークン更新」は即時にtoken endpointへアクセスし、ネットワーク/IdP利用不可を表示する。自動更新は意図的に実装していない
- 新しいブラウザプロファイルでAのログインをクリックし、Keycloakへ到達できないことを確認する。既存Webセッションの画面表示と新規ログイン成功を混同しない

次にAPI Bだけ再起動してcold cacheを作る。

```bash
docker compose restart api-b
curl -i -H "Authorization: Bearer $(cat generated/alice-b.token)" http://127.0.0.1:9002/api/me
```

### 期待結果

| 操作 | 期待結果 |
|---|---|
| 停止直後、warm cacheで有効JWT | API A/Bとも200 |
| 新規ログイン | Keycloakに接続できず失敗 |
| Webの更新ボタン | Refresh unavailableと表示 |
| API B再起動後のcold cache | 401（数秒以内の取得失敗） |
| 再起動しないAPI A | tokenの期限内200 |
| exp超過 | warm cacheでも401 |
| /healthz | Aは200、BはDBが動けば200。IdP readinessは表さない |

### 判定の注意

- APIへのcurlは再起動完了後に実行する。瞬間的なconnection refusedはcold-cache判定ではない
- 180秒の期限と起動時間を記録してから比較する
- 期限切れは元のexpを過ぎてから確認し、payloadを編集しない

### 復旧

```bash
docker compose start keycloak
python3 scripts/start.py
```

復旧後、既存IdPセッションが残っていれば更新可能。セッション期限を超えた場合は再ログインする。API Bも有効JWTを送れば鍵を取得し200へ戻る。

### 自動で行う場合

```bash
python3 scripts/failure_test.py            # 新規ログイン、更新失敗、warm/cold cacheを確認し、finallyでKeycloakを再起動する
python3 scripts/failure_test.py --expiry   # 実際の180秒の期限切れも待つ
```

教材サービスを停止・再起動するので、他の試験と同時実行しない。中断で復旧しなかった場合はstart.pyを実行する。

### なぜ行うのか

api-bの再起動でメモリ内JWKSキャッシュが消える。API Bの初回認証はJWKS取得が必要となり失敗する。API Aは再起動せず比較する。

## Step 8. 鍵ローテーション

### やること

Realmに優先順位の高い署名鍵を追加する。

1. 管理画面のRealm settings → Keys → Providers → rsa-generatedを追加する
2. Algorithm RS256、Enabled/ActiveをON、Priorityを既存のRSA providerより高く設定する
3. A/Bでtoken更新し、新しいkidになることをローカルで確認する（decodeは観察用であり検証ではない）
4. API再起動なしでmeを呼ぶ

### 期待結果

新鍵の正常tokenが200。旧鍵は旧tokenの期限が切れるまで公開鍵を残す。未知鍵取得先が停止したケースはGoの `TestJWTAndJWKS` でも自動検証する。

※ 未検証（管理画面での操作）。同じ鍵の追加を管理CLIで実施し、新しいkidのtokenがAPI再起動なしで200になることを確認した。

### なぜ行うのか

go-oidcのRemoteKeySetはプロセス内の公開鍵を利用し、署名を検証できない場合にJWKSを再取得する。独自の固定TTL、永続キャッシュ、定期取得は追加していない。ネットワーク障害時に未知鍵を信用しない。

---

次へ：[Chapter 06: 振り返り](./06-retrospective.md)
