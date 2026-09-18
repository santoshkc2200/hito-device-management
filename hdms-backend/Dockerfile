# Production image for hdms-api (Phase 5.3a).
#
# Multi-stage, fully pinned by digest, runs as a non-root user, and ships
# only the compiled binaries — no bind-mounted source, no air, no toolchain.
# Migrations run on startup from the goose set embedded in the binary
# (internal/platform/db.Migrate under the advisory lock), so deploying is
# just "run the image".
#
# Build with: docker build -f hdms-backend/Dockerfile hdms-backend
# Or via:     docker compose -f deploy/production/compose.yaml build api

# Builder: same Go toolchain as Dockerfile.dev, pinned by index digest.
FROM golang:1.26-alpine@sha256:ce864e7223ac17b1775e6fd0b4c0db580c2eb50e7953a427916379e4b92a1628 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static, trimmed binary — no cgo, no VCS stamping surprises.
RUN CGO_ENABLED=0 go build -trimpath -o /out/hdms-api ./cmd/hdms-api && \
    CGO_ENABLED=0 go build -trimpath -o /out/hdms-cli ./cmd/hdms-cli

# Runtime: minimal Alpine, pinned by index digest, with root CAs so the
# Entra OIDC discovery (HTTPS) and Postgres sslmode=verify-full both work.
FROM alpine:3.22@sha256:87ba4a8b898045643099c4f2f8771dece62048755ecd2d6fc2ec4acdb95d2f2a

RUN apk add --no-cache ca-certificates wget && \
    addgroup -S hdms && adduser -S hdms -G hdms

COPY --from=builder /out/hdms-api /usr/local/bin/hdms-api
COPY --from=builder /out/hdms-cli /usr/local/bin/hdms-cli

USER hdms

EXPOSE 8443

ENTRYPOINT ["/usr/local/bin/hdms-api"]
