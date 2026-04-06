# ── Stage 1: build ───────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w -extldflags '-static'" \
    -o /app/bin/chukuago-api ./cmd/server

# ── Stage 2: minimal runtime ─────────────────────────────────────────────────
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /app/bin/chukuago-api /chukuago-api
COPY --from=builder /app/migrations /migrations

ENV TZ=Africa/Nairobi

EXPOSE 8080

ENTRYPOINT ["/chukuago-api"]
