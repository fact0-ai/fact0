# ─── Build Stage ─────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/fact0 ./fact0
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/fact0-migrate ./fact0-migrate

# ─── Runtime Stage ───────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget

COPY --from=builder /bin/fact0 /bin/fact0
COPY --from=builder /bin/fact0-migrate /bin/fact0-migrate
COPY LICENSE THIRD_PARTY_LICENSES.txt /usr/share/licenses/fact0/

# Structured logs are readable by local container logging tools.
ENV FACT0_LOG_FORMAT=json

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO- http://localhost:8000/health || exit 1

ENTRYPOINT ["/bin/fact0"]
