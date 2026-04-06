# ChukuaGo API

> **Chukua** (Swahili) — *take, pick up*. Someone picks it up so you don't have to.

A two-sided errand marketplace backend connecting busy Kenyans with vetted local runners. Built in Go, designed for Nairobi, architected to scale countrywide.

---

## Table of Contents

- [Stack](#stack)
- [Project Structure](#project-structure)
- [Quick Start](#quick-start)
- [Environment Variables](#environment-variables)
- [API Overview](#api-overview)
- [Authentication Flow](#authentication-flow)
- [Errand Lifecycle](#errand-lifecycle)
- [OTP Delivery Confirmation](#otp-delivery-confirmation)
- [Third-Party Services](#third-party-services)
- [Running Tests](#running-tests)
- [Database Migrations](#database-migrations)
- [Deployment](#deployment)

---

## Stack

| Layer              | Technology                        |
|--------------------|-----------------------------------|
| Language           | Go 1.22                           |
| Router             | chi v5                            |
| Database           | PostgreSQL 15 + PostGIS           |
| Cache / Rate limit | Redis 7 (Upstash in production)   |
| Auth               | SMS OTP → JWT (access + refresh)  |
| SMS / OTP          | **Africa's Talking**              |
| Push notifications | **Firebase Cloud Messaging (FCM)**|
| File storage       | AWS S3 (af-south-1)               |
| Logging            | Zap (structured JSON)             |
| Deployment         | Render (API) / Docker             |

---

## Project Structure

```
chukuago-api/
├── cmd/server/         Entry point, router wiring, graceful shutdown
├── internal/
│   ├── auth/           OTP flow, JWT, session lifecycle
│   ├── users/          Profile CRUD, location updates
│   ├── runners/        KYC submission, availability, service areas
│   ├── errands/        Errand CRUD, FSM, runner feed (PostGIS)
│   ├── offers/         Bidding, instant accept, SELECT FOR UPDATE
│   ├── delivery/       OTP proof of delivery (SHA-256, never raw)
│   ├── chat/           WebSocket hub, message persistence
│   ├── reviews/        Post-completion ratings, runner avg recalc
│   ├── disputes/       Open dispute, admin resolution
│   ├── notifications/  FCM push + in-app notification store
│   ├── admin/          KYC review, user management, audit logs
│   └── middleware/     JWT auth, role guard, Redis rate limiter
├── pkg/
│   ├── config/         Env-driven config with panic on missing vars
│   ├── database/       pgxpool setup + golang-migrate runner
│   ├── pagination/     Reusable page/limit parsing
│   ├── redis/          Redis client factory
│   ├── response/       Consistent JSON envelope helpers
│   ├── storage/        S3 uploader with MIME validation
│   ├── types/          Shared domain structs, enums, context helpers
│   └── validator/      JSON decode + go-playground/validator
└── migrations/         SQL migrations (golang-migrate, up + down)
```

Each `internal/` package follows the same layout:

```
dto.go        Request/response structs — no business logic
errors.go     Typed sentinel errors for this domain
store.go      Data access layer — SQL only, no business logic
{domain}.go   Service (orchestration) + Handler (HTTP) in one file
*_test.go     Unit tests
```

---

## Quick Start

```bash
# 1. Clone and install dependencies
git clone https://github.com/chukuago/api
cd chukuago-api
go mod download

# 2. Start Postgres (with PostGIS) and Redis
make docker-up

# 3. Copy and fill in env vars
cp .env.example .env
# At minimum set AT_API_KEY, AT_USERNAME, FIREBASE_CRED_JSON, S3_* vars

# 4. Run migrations
make migrate-up

# 5. Start the server
make run
# → listening on :8080
```

---

## Environment Variables

See [`.env.example`](.env.example) for the full list with comments.

**Required in production:**

| Variable            | Description                                      |
|---------------------|--------------------------------------------------|
| `DATABASE_URL`      | PostgreSQL DSN                                   |
| `REDIS_URL`         | Redis connection URL                             |
| `JWT_SECRET`        | ≥ 32-byte random string                          |
| `AT_API_KEY`        | Africa's Talking API key                         |
| `AT_USERNAME`       | Africa's Talking account username                |
| `FIREBASE_CRED_JSON`| Service account JSON (single line)               |
| `S3_BUCKET`         | S3 bucket name for uploads                       |
| `S3_ACCESS_KEY`     | AWS access key                                   |
| `S3_SECRET_KEY`     | AWS secret key                                   |

---

## API Overview

All routes are prefixed `/api/v1`. Every response follows:

```json
// Success
{ "success": true, "data": { ... } }

// List
{ "success": true, "data": [...], "meta": { "page": 1, "limit": 20, "total": 143 } }

// Error
{ "success": false, "error": { "code": "ERRAND_NOT_FOUND", "message": "..." } }
```

### Auth

| Method | Route                     | Description                  |
|--------|---------------------------|------------------------------|
| POST   | `/auth/send-otp`          | Send 6-digit OTP via SMS     |
| POST   | `/auth/verify-otp`        | Verify OTP → receive tokens  |
| POST   | `/auth/refresh`           | Rotate refresh token         |
| POST   | `/auth/logout`            | Revoke current session       |

### Profile

| Method | Route                     | Description                  |
|--------|---------------------------|------------------------------|
| GET    | `/profile`                | Get current user + profile   |
| PATCH  | `/profile`                | Update name / bio / avatar   |
| PATCH  | `/profile/location`       | Update GPS coordinates       |

### Errands (client)

| Method | Route                                    | Description             |
|--------|------------------------------------------|-------------------------|
| POST   | `/errands`                               | Create errand + stops   |
| GET    | `/errands`                               | List own errands        |
| GET    | `/errands/:id`                           | Get errand detail       |
| PATCH  | `/errands/:id`                           | Edit (pre-assignment)   |
| DELETE | `/errands/:id`                           | Cancel errand           |
| POST   | `/errands/:id/stops`                     | Add a stop              |
| GET    | `/errands/:id/offers`                    | View bids               |
| POST   | `/errands/:id/offers/:offerId/accept`    | Accept a runner         |
| POST   | `/errands/:id/delivery-otp`             | Generate delivery code  |
| POST   | `/errands/:id/verify-delivery`          | Confirm delivery (OTP)  |

### Runner

| Method | Route                                | Description             |
|--------|--------------------------------------|-------------------------|
| POST   | `/runner/kyc`                        | Submit KYC documents    |
| GET    | `/runner/kyc/status`                 | Check KYC status        |
| PATCH  | `/runner/availability`               | Toggle online/offline   |
| POST   | `/runner/service-areas`              | Add service region      |
| DELETE | `/runner/service-areas/:id`          | Remove service region   |
| POST   | `/runner/errands/feed`               | Browse available errands|
| POST   | `/runner/errands/:id/offers`         | Place a bid             |
| PATCH  | `/runner/errands/:id/status`         | Advance errand status   |

---

## Authentication Flow

```
Client                          Server                    Africa's Talking
  │                               │                              │
  │  POST /auth/send-otp          │                              │
  │  { phone, role }              │                              │
  │──────────────────────────────►│                              │
  │                               │  SendSMS(phone, "Your code: 482910")
  │                               │─────────────────────────────►│
  │                           200 │                              │
  │◄──────────────────────────────│                              │
  │                               │                              │
  │  POST /auth/verify-otp        │                              │
  │  { phone, otp }               │                              │
  │──────────────────────────────►│                              │
  │                               │  Redis.Get("otp:+254...")    │
  │                               │  compare SHA-256 hash        │
  │                               │  upsert user                 │
  │  { accessToken, refreshToken }│                              │
  │◄──────────────────────────────│                              │
```

- OTPs are stored in Redis with a 10-minute TTL
- Max 3 wrong attempts before the OTP is locked
- Refresh tokens are rotated on every use (session rotation)
- All sessions are stored in `user_sessions` and can be revoked

---

## Errand Lifecycle

```
draft ──► posted ──► bidding ──► assigned ──► in_progress ──► delivered ──► completed
                 └──────────────────────┘                               └──► disputed
                      (instant accept)
```

The FSM is enforced server-side in `internal/errands/errors.go`. Any attempt to perform an invalid transition returns `409 INVALID_TRANSITION`.

---

## OTP Delivery Confirmation

The delivery OTP is the platform's trust mechanism replacing escrow in the MVP:

```
Runner marks errand "delivered"
  → Client taps "Generate Code"
  → Server generates 6-digit OTP, stores SHA-256 hash (never raw), sets 10-min TTL
  → Client shares code verbally with runner
  → Runner enters code in app
  → Server compares hash
  → On match: errand → "completed", both parties notified via FCM
```

Security properties:
- Raw OTP is **never** stored anywhere — only `SHA-256(otp)`
- Max 3 attempts before OTP locks (client must regenerate)
- Regenerating a code atomically invalidates the previous one
- Only the assigned runner can submit the OTP

---

## Third-Party Services

### Africa's Talking (SMS / OTP)
Sign up at [africastalking.com](https://africastalking.com).
Use `sandbox` username + any API key for local development.
The same account will be used for M-Pesa STK push in Phase 2.

### Firebase Cloud Messaging (Push Notifications)
1. Create a Firebase project at [console.firebase.google.com](https://console.firebase.google.com)
2. Project Settings → Service Accounts → Generate new private key
3. Paste the JSON as `FIREBASE_CRED_JSON` (single line, escaped)
4. The Flutter app registers the FCM token on login and stores it via a `PATCH /profile` extension

### AWS S3 (File Storage)
Use `af-south-1` (Cape Town) for lowest latency from Kenya.
KYC documents and chat images are uploaded directly from the mobile client using presigned URLs — the API only receives the resulting S3 URL and validates it before storing.

---

## Running Tests

```bash
# All tests
make test

# Specific package
go test ./internal/errands/... -v -run TestIsValidTransition

# With race detector (always on in CI)
go test ./... -race

# Coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

---

## Database Migrations

```bash
# Apply all pending migrations
make migrate-up

# Roll back one migration
make migrate-down

# Create a new migration
make migrate-create
# → prompted for name, creates 00X_name.up.sql and 00X_name.down.sql

# Check current version
make migrate-status
```

Migrations use [golang-migrate](https://github.com/golang-migrate/migrate). They are automatically applied on server startup in non-production environments.

---

## Deployment

### Render (recommended for MVP)

1. Connect your GitHub repo to Render
2. Set all environment variables in the Render dashboard
3. Set build command: `go build -o bin/chukuago-api ./cmd/server`
4. Set start command: `./bin/chukuago-api`
5. Add a managed PostgreSQL and Redis instance from Render's marketplace

### Docker

```bash
docker build -t chukuago-api .
docker run -p 8080:8080 --env-file .env chukuago-api
```

The final image is built on `scratch` — it is ~12 MB and contains only the statically-linked binary and migrations.

---

*ChukuaGo — Built in Nairobi, for Nairobi, scaling Kenya.*
