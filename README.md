# PS188 Backend

Backend for **PS188 — AI-Based Fake Identity & Document Screening System**
(SIH 2026, PS 26188 — Ministry of Home Affairs / SSB).

A border-checkpoint officer (a `supervisor`-role user) submits an identity/travel
document; the backend stores the case, calls an externally-hosted AI screening model,
persists the **risk score + explainable verdict**, and lets them record a manual
decision — with a full append-only audit trail. Two roles: `supervisor` (checkpoint)
and `admin` (account management).

**Stack:** Go 1.26 · Gin · MongoDB (`mongo-driver/v2`) · JWT · Docker Compose.
The AI model itself lives in `../Al-Based-Fake-Identity-Document-Screening-System/`
(`predict_pipeline.py`), wrapped in a **separate hosted FastAPI service** this backend
calls over HTTP.

## Docs

| File | What |
|---|---|
| [`docs/backend/BACKEND_GUIDE.md`](docs/backend/BACKEND_GUIDE.md) | Code style & architecture bible — error convention, repositories, services, handlers, testing, Docker |
| [`docs/backend/backend-architecture.md`](docs/backend/backend-architecture.md) | Module map, every endpoint, the screening lifecycle, error codes, build order |
| [`docs/backend/database-design.md`](docs/backend/database-design.md) | MongoDB collections, document shapes, indexes, design decisions |

## Quick start (Docker)

```bash
cp .env.example .env.development.local     # already present for local dev
docker compose up --build
```

- MongoDB comes up first (healthcheck-gated), then the backend on **:8080**.
- `SCREENING_ENGINE=mock` by default — the stack runs with **no external model**.
  Set `SCREENING_ENGINE=http` + `SCREENING_SERVICE_URL=...` to use the real FastAPI model.
- First boot seeds an admin: `ADMIN_USERNAME` / `ADMIN_PASSWORD` (`admin` / `admin12345`).

```bash
curl localhost:8080/health

# login as the seeded admin
curl -s -XPOST localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin12345"}'

# create a supervisor (admin token)
curl -s -XPOST localhost:8080/api/users -H "Authorization: Bearer $ADMIN" \
  -H 'Content-Type: application/json' \
  -d '{"username":"sup.jane","full_name":"Jane","email":"jane@ps188.local","password":"jane12345","role":"supervisor"}'

# submit a screening (supervisor token)
curl -s -XPOST localhost:8080/api/screenings -H "Authorization: Bearer $SUPERVISOR" \
  -F document=@../Al-Based-Fake-Identity-Document-Screening-System/sample/passport/download.jpg \
  -F doc_type=passport -F doc_number=Z1234567 -F checkpoint_id=CP-1

curl -s localhost:8080/api/screenings -H "Authorization: Bearer $SUPERVISOR"
```

## Local development (no Docker)

Needs a MongoDB on `localhost:27017` (e.g. `docker run -p 27017:27017 mongo:7`).

```bash
make run       # go run ./cmd/api
make test      # go test ./... against the local MongoDB (skips cleanly if none)
make build     # bin/api
```

## Layout

```
cmd/api/            process entrypoint + lifecycle
internal/config     env → Config
internal/database   Mongo connect + EnsureIndexes
internal/apperr     AppError + ERRORS catalog
internal/model      User, Screening, AuditLog
internal/repository Mongo data access (interfaces + impls)
internal/service    business logic (auth, users, screening orchestration)
internal/screening  external FastAPI model client (http + mock)
internal/storage    GridFS document-image store
internal/transport/http  Gin router + handlers
internal/middleware auth, error, cors, rate-limit, request log
internal/platform/jwt    token manager
pkg/logger          slog factory
```

See `docs/backend/BACKEND_GUIDE.md` for the rules that govern every file here.
