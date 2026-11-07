# ビルド用ステージ。依存の取得を先に行い、キャッシュを効かせる。
FROM golang:1.27.3-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /lab ./cmd/lab

# 実行用ステージ。シェルのないdistrolessの非rootイメージに、バイナリだけを入れる。
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /lab /lab
EXPOSE 8080
ENTRYPOINT ["/lab"]
