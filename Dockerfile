# The one image the app runs from, locally (docker compose) and anywhere else.

# The Go version matches the go directive in go.mod.
FROM golang:1.27.3-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Pure Go (modernc.org/sqlite needs no cgo), so the binary runs on a static base.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/go-slow ./cmd/go-slow
# The data directory exists in the image owned by the nonroot user, so a named
# volume mounted over it starts out writable.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
WORKDIR /app
COPY --from=build /out/go-slow /app/go-slow
COPY --from=build --chown=nonroot:nonroot /out/data /app/data
USER nonroot
ENTRYPOINT ["/app/go-slow"]
