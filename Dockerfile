# hadolint ignore=DL3007
FROM cgr.dev/chainguard/go:latest AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o girus-server main.go

# hadolint ignore=DL3007
FROM cgr.dev/chainguard/static:latest

ARG revision

LABEL \
  org.opencontainers.image.title="Girus Backend" \
  org.opencontainers.image.description="Backend for the Girus application" \
  org.opencontainers.image.authors="Eduardo Thums <eduardocristiano01@gmail.com>" \
  org.opencontainers.image.licenses="MIT" \
  org.opencontainers.image.version="1.0.0" \
  org.opencontainers.image.url="https://linuxtips.io/girus-labs/" \
  org.opencontainers.image.source="https://github.com/eduardothums/girus-pick" \
  org.opencontainers.image.documentation="https://github.com/eduardothums/girus-pick/README.md" \
  org.opencontainers.image.revision="$revision"

COPY --from=builder /app/girus-server /usr/bin/

ENV PORT=8080
ENV GIN_MODE=release

EXPOSE $PORT

ENTRYPOINT ["/usr/bin/girus-server"]
