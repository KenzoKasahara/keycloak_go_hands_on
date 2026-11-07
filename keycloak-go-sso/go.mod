// 依存の更新で結果が変わらないよう、バージョンを固定している。
module sso-lab

go 1.27.3

// 教材が直接使う依存。
//   - go-oidc：JWT検証とDiscovery
//   - pgx：PostgreSQLドライバ
//   - oauth2：認可コードフローとPKCE
require (
	github.com/coreos/go-oidc/v3 v3.14.1
	github.com/jackc/pgx/v5 v5.7.5
	golang.org/x/oauth2 v0.28.0
)

// 上記のライブラリが間接的に使う依存。
require (
	github.com/go-jose/go-jose/v4 v4.0.5 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/text v0.24.0 // indirect
)
