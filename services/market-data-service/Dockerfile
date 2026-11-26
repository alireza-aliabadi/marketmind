# Initial verified target: Linux amd64. Upgrade pinned image digests deliberately.
FROM golang:1.26.11-bookworm@sha256:79ecd0a7157c7a4cf3b281ced8465350591bf54fedab540e75374a003e1fde7e AS build
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=readonly -trimpath -ldflags="-s -w" -o /market-data ./cmd/market-data

FROM chainguard/static@sha256:f68e3a8244c7d0f4cd56635aaff8e6a533cf6cc3850d8fb339567a5782d6a0b0
COPY --from=build /market-data /market-data
USER 65532:65532
ENTRYPOINT ["/market-data"]
