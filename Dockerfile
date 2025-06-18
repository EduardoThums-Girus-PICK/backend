# go:1.24.4
FROM cgr.dev/chainguard/go:latest@sha256:3930f1a74c286cd1ee1d2f4a74b0fc48d0ca36ffdf76e39918161cc6576ccc9d AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o healthcheck ./healthcheck
# hadolint ignore=DL3059
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./server

FROM cgr.dev/chainguard/static:latest@sha256:797e62f43d04d792e9f930913e7d9f5a63e92bd19ca5e7e5139a692decee2dbc

ARG revision
ARG version

LABEL \
  org.opencontainers.image.title="Girus Backend" \
  org.opencontainers.image.description="Backend for the Girus application" \
  org.opencontainers.image.authors="Eduardo Thums <eduardocristiano01@gmail.com>" \
  org.opencontainers.image.licenses="MIT" \
  org.opencontainers.image.version="$version" \
  org.opencontainers.image.url="https://linuxtips.io/girus-labs/" \
  org.opencontainers.image.source="https://github.com/EduardoThums-Girus-PICK/backend" \
  org.opencontainers.image.documentation="https://github.com/EduardoThums-Girus-PICK/backend/README.md" \
  org.opencontainers.image.revision="$revision"

COPY --from=builder /app/server/server /app/healthcheck/healthcheck /usr/bin/

ENV PORT=8080
ENV GIN_MODE=release

HEALTHCHECK --interval=30s --timeout=10s --start-period=2s --retries=5 CMD ["/usr/bin/healthcheck"]
EXPOSE $PORT

ENTRYPOINT ["/usr/bin/server"]
