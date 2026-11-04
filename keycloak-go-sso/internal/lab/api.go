// lab はWeb/APIの実装をまとめる。api.go はAPI側（JWT検証、RBAC、所有者認可）を担当する。
package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Identity は検証済みAccess Tokenから取り出す、認可に必要なclaims。
type Identity struct {
	Subject   string `json:"sub"`
	Type      string `json:"typ"`
	NotBefore int64  `json:"nbf"`
	Realm     struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

// identityKey は他パッケージのcontextキーと衝突しないための専用型。
type identityKey struct{}

// Admin はRealmロールにadminを持つかどうかを返す。
func (identity Identity) Admin() bool {
	for _, role := range identity.Realm.Roles {
		if role == "admin" {
			return true
		}
	}
	return false
}

// JSON はキャッシュさせない設定でJSONレスポンスを書き出す。
func JSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(writer).Encode(value)
}

// Authenticate はBearerトークンを検証し、成功時だけIdentityをcontextへ入れて次へ渡すMiddleware。
func Authenticate(verifier *oidc.IDTokenVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		deny := func() {
			writer.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(writer, "unauthorized", 401)
		}

		parts := strings.Fields(request.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			deny()
			return
		}

		// 署名・iss・aud・expはgo-oidcが検証する。
		token, err := verifier.Verify(request.Context(), parts[1])
		if err != nil {
			log.Printf("token verification failed: %v", err)
			deny()
			return
		}

		// ライブラリが見ないsub、payloadのtyp、nbfは自前で確認する。
		// typ=Bearerの要求により、audが一致するID Tokenなどを拒否する。
		var identity Identity
		claimsErr := token.Claims(&identity)
		if claimsErr != nil ||
			identity.Subject == "" ||
			identity.Type != "Bearer" ||
			identity.NotBefore > time.Now().Unix() {
			log.Printf(
				"access token claims rejected: decode=%v missing_sub=%t invalid_type=%t future_nbf=%t",
				claimsErr,
				identity.Subject == "",
				identity.Type != "Bearer",
				identity.NotBefore > time.Now().Unix(),
			)
			deny()
			return
		}

		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), identityKey{}, identity)))
	})
}

// Document は所有者付きの文書。ownerにはユーザー名ではなく不変のsubを使う。
type Document struct {
	ID    int64  `json:"id"`
	Owner string `json:"owner_sub"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Documents はテストでfakeへ差し替えるためのRepository。
type Documents interface {
	List(context.Context, string) ([]Document, error)
	Get(context.Context, int64, string, bool) (Document, error)
}

// SQLDocuments はPostgreSQLを使うDocumentsの実装。
type SQLDocuments struct{ Database *sql.DB }

// List は本人の文書だけを返す。adminでも一覧は本人に限定する。
func (repository SQLDocuments) List(ctx context.Context, owner string) ([]Document, error) {
	rows, err := repository.Database.QueryContext(
		ctx,
		"SELECT id,owner_sub,title,body FROM documents WHERE owner_sub=$1 ORDER BY id",
		owner,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	documents := []Document{}
	for rows.Next() {
		var document Document
		if err := rows.Scan(&document.ID, &document.Owner, &document.Title, &document.Body); err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return documents, rows.Err()
}

// Get は本人の文書、またはadminの場合に限り他者の文書を返す。
// 権限がない場合も不存在と同じsql.ErrNoRowsになる。
func (repository SQLDocuments) Get(ctx context.Context, id int64, owner string, admin bool) (Document, error) {
	var document Document
	err := repository.Database.QueryRowContext(
		ctx,
		"SELECT id,owner_sub,title,body FROM documents WHERE id=$1 AND (owner_sub=$2 OR $3)",
		id,
		owner,
		admin,
	).Scan(&document.ID, &document.Owner, &document.Title, &document.Body)
	return document, err
}

// Protected は認証済みリクエストを処理するルーターを返す。文書APIはapi-bだけに登録する。
func Protected(audience string, repository Documents) http.Handler {
	router := http.NewServeMux()

	router.HandleFunc("GET /api/me", func(writer http.ResponseWriter, request *http.Request) {
		JSON(writer, request.Context().Value(identityKey{}))
	})

	// RBAC：JWTが正常でもadminロールがなければ403。
	router.HandleFunc("GET /api/admin", func(writer http.ResponseWriter, request *http.Request) {
		if !request.Context().Value(identityKey{}).(Identity).Admin() {
			http.Error(writer, "forbidden", 403)
			return
		}
		JSON(writer, map[string]string{"message": "admin allowed", "api": audience})
	})

	if audience == "api-b" {
		router.HandleFunc("GET /api/documents", func(writer http.ResponseWriter, request *http.Request) {
			identity := request.Context().Value(identityKey{}).(Identity)

			documents, err := repository.List(request.Context(), identity.Subject)
			if err != nil {
				http.Error(writer, "database error", 500)
				return
			}
			JSON(writer, documents)
		})

		router.HandleFunc("GET /api/documents/{id}", func(writer http.ResponseWriter, request *http.Request) {
			id, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
			if err != nil || id <= 0 {
				http.Error(writer, "invalid id", 400)
				return
			}

			identity := request.Context().Value(identityKey{}).(Identity)
			document, err := repository.Get(request.Context(), id, identity.Subject, identity.Admin())

			// 他者の文書と不存在を同じ404にして、IDの存在を漏らさない。
			if err == sql.ErrNoRows {
				http.NotFound(writer, request)
				return
			}
			if err != nil {
				http.Error(writer, "database error", 500)
				return
			}
			JSON(writer, document)
		})
	}

	return router
}

// NewAPI はAPIのハンドラーを組み立てる。audienceごとに受け入れるトークンを固定する。
func NewAPI(ctx context.Context, issuer, audience, databaseURL string) (http.Handler, error) {
	if audience != "api-a" && audience != "api-b" {
		return nil, fmt.Errorf("invalid audience")
	}

	// Discoveryを使わず既知のJWKS URLを指定し、IdP停止中でも起動できるようにする。
	// 公開鍵はメモリにキャッシュされ、未知の鍵のときだけ再取得される。
	keys := oidc.NewRemoteKeySet(ctx, issuer+"/protocol/openid-connect/certs")

	// 名前はIDTokenVerifierだが、ClientIDにAPIのaudienceを指定してAccess Tokenの検証に使う。
	verifier := oidc.NewVerifier(issuer, keys, &oidc.Config{
		ClientID:             audience,
		SupportedSigningAlgs: []string{"RS256"},
	})

	// DBを使うのはapi-bだけ。
	var database *sql.DB
	if audience == "api-b" {
		var err error
		database, err = sql.Open("pgx", databaseURL)
		if err != nil {
			return nil, err
		}

		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := database.PingContext(check); err != nil {
			database.Close()
			return nil, err
		}
	}

	router := http.NewServeMux()

	// healthz はAPIプロセスとDBの状態だけを表し、IdPの状態は表さない。
	router.HandleFunc("GET /healthz", func(writer http.ResponseWriter, request *http.Request) {
		if database != nil {
			check, cancel := context.WithTimeout(request.Context(), time.Second)
			defer cancel()
			if database.PingContext(check) != nil {
				http.Error(writer, "database unavailable", 503)
				return
			}
		}
		fmt.Fprintln(writer, "ok")
	})

	router.Handle("/api/", Authenticate(verifier, Protected(audience, SQLDocuments{Database: database})))

	return router, nil
}
