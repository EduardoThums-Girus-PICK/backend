# go:1.24.4
FROM cgr.dev/chainguard/go:latest@sha256:0edc57c11262ef29f7da3b132becfe811cf44502c010872804d8fe293ab8ba4c AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o healthcheck ./healthcheck
# hadolint ignore=DL3059
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./server

FROM cgr.dev/chainguard/static:latest@sha256:6a4b683f4708f1f167ba218e31fcac0b7515d94c33c3acf223c36d5c6acd3783

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
