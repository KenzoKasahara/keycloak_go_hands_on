# Chapter 07: 片付けと次の実験

## この章の目的

サービスと永続データ、ローカル認証情報を片付ける。費用の考え方と、次に検証できることを確認する。

## 現在地

準備と仮説 → 環境構築 → SSOと認可の観察 → 自動テストと機能検証 → 障害試験 → 振り返り → **片付け**

## 完了条件

- [ ] 教材のコンテナとDB Volumeがない
- [ ] `.env`、`generated/`、ダウンロードしたtoken、ブラウザのサイトデータを削除した

## Step 9. 学習リソースを削除する

### やること

保存したい学習記録を退避してから、教材の6サービス、ネットワーク、DB Volumeを削除する。

### 実行

```bash
docker compose down --volumes --remove-orphans
docker compose ps -a
docker volume ls --filter name=keycloak-go-sso-lab_pg-data
```

### 期待結果

`docker compose ps -a` に教材コンテナがなく、`docker volume ls` に対象Volumeがない。

続けて次を手動で削除する。

- `.env` と `generated/`
- ブラウザからダウンロードしたtoken
- ブラウザの127.0.0.1のサイトデータ

検証用PythonのCookie jarはプロセス終了で破棄される。アプリのメモリセッションはコンテナ削除で消える。

### なぜ行うのか

通常のdownだけではVolumeが残り、次回のRealm importやDB初期化が再実行されない。

### 残るものと再開

| 目的 | 操作 |
|---|---|
| イメージ・キャッシュの削除 | イメージ・ビルドキャッシュ・Goモジュールキャッシュは残る。必要なものだけ個別削除し、共有環境で `docker system prune` を一括実行しない |
| 停止だけ | `docker compose stop` |
| 再開 | .envとVolumeを対応させたまま `python3 scripts/start.py` |
| 完全初期化 | Volume削除後に.env/generatedを削除し、setupからやり直す |

## 費用

本構成はクラウドAPIを使用せずクラウド資源も作成しないため、構成自体のクラウド従量課金は発生しない。PCの電力、通信、ディスク、Docker Desktop等の利用契約は別。商用利用条件は所属組織で確認する。ライセンス費用の具体額は本書では見積もらない。

コンテナ停止後もVolume、イメージ、ビルドキャッシュはディスクを占有する。クラウドVM上で実行する場合はVM、ディスク、IP、転送量等の費用が発生し、Compose downだけではVMやディスクの課金を止められない。クラウド実行は本教材の構築手順には含めない。

## 次の実験

優先順に次へ進む。

1. Back-Channel Logout
2. Confidential Client + PKCE
3. HTTPSと独立ホスト名
4. MFA
5. client roles
6. セッション外部化
7. Keycloak/DBバックアップ復元
8. 複数Replica
9. 未知kid大量送信時の保護
10. introspectionとJWT検証の比較

本番化ではSecret Manager等、管理者権限保護、監査ログ、rate limit、DB最小権限、パッチ・脆弱性評価、可用性設計を追加する。

独自認証との性能・工数比較を行うなら、対象機能、負荷、ハードウェア、測定方法、失敗条件をそろえて計測する。本書の定性比較を数値へ換算しない。

## 参考資料

- [Keycloak hostname設定](https://www.keycloak.org/server/hostname)：公開URLとバックチャネルの扱い
- [Keycloakコンテナ](https://www.keycloak.org/server/containers)：開発モードと本番構成
- [Realm import/export](https://www.keycloak.org/server/importExport)：初回import、環境変数プレースホルダー
- [Server Administration Guide 26.7.0](https://www.keycloak.org/docs/26.7.0/server_admin/index.html)：Client、Role、Audience、SSO
- [go-oidc v3.14.1](https://github.com/coreos/go-oidc/tree/v3.14.1)：検証とRemoteKeySet実装
- [OIDC RP-Initiated Logout](https://openid.net/specs/openid-connect-rpinitiated-1_0.html)：IdPログアウト
