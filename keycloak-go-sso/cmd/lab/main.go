// lab はWeb/APIの共通バイナリ。起動引数でどちらとして動くかを選ぶ。
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"sso-lab/internal/lab"
)

// main は引数に応じてAPIまたはWebのハンドラーを組み立て、HTTPサーバーを起動する。
func main() {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: 3 * time.Second}

	// issuerのURLはブラウザと共通の 127.0.0.1:9080 に固定する。コンテナ内からはそこへ届かないので、
	// 接続先だけをKeycloakコンテナへ置き換える。URL・Hostヘッダー・期待issuerは変えない。
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "127.0.0.1:9080" && os.Getenv("KEYCLOAK_INTERNAL") != "" {
			address = os.Getenv("KEYCLOAK_INTERNAL")
		}
		return dialer.DialContext(ctx, network, address)
	}

	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	// go-oidcが内部で行うHTTP通信にも、上の接続先置き換えを適用する。
	ctx := oidc.ClientContext(context.Background(), client)

	var handler http.Handler
	var err error

	if len(os.Args) != 2 {
		log.Fatal("usage: lab api|web")
	}

	switch os.Args[1] {
	case "api":
		handler, err = lab.NewAPI(
			ctx,
			os.Getenv("ISSUER"),
			os.Getenv("API_AUDIENCE"),
			os.Getenv("DATABASE_URL"),
		)
	case "web":
		handler, err = lab.NewWeb(
			ctx,
			client,
			os.Getenv("ISSUER"),
			os.Getenv("CLIENT_ID"),
			os.Getenv("PUBLIC_BASE"),
			os.Getenv("API_URL"),
			os.Getenv("OTHER_API_URL"),
		)
	default:
		log.Fatal("unknown mode")
	}
	if err != nil {
		log.Fatal(err)
	}

	// Slowloris等で接続を占有されないよう、各タイムアウトを明示する。
	server := &http.Server{
		Addr:              ":8080",
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}
