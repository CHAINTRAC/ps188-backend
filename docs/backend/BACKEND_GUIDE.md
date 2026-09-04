# PS188 Backend — Code Style & Architecture Guide

> **Single source of truth for every line of Go written for this backend.**
> Every handler, service, repository, model, middleware and test MUST follow these
> patterns. If something is not covered here, follow the closest existing pattern
> and stay consistent. Do not invent new patterns.

**Project:** PS188 — AI-Based Fake Identity & Document Screening System.
**Stack:** Go 1.26 · Gin · MongoDB (official `mongo-driver/v2`) · JWT · Docker Compose.
**Companion docs:** `backend-architecture.md` (what we build — modules, endpoints,
state machine), `database-design.md` (MongoDB collections & indexes).

---

## Table of Contents

1. [Project Structure](#1-project-structure)
2. [Technology Stack](#2-technology-stack)
3. [Environment & Config](#3-environment--config)
4. [The Error Convention — `(T, error)` + `AppError`](#4-the-error-convention)
5. [Response Envelopes](#5-response-envelopes)
6. [Cursor Pagination](#6-cursor-pagination)
7. [Models](#7-models)
8. [Repositories](#8-repositories)
9. [Services](#9-services)
10. [Transport / HTTP Handlers](#10-transport--http-handlers)
11. [Middleware](#11-middleware)
12. [The External Screening Engine](#12-the-external-screening-engine)
13. [MongoDB Setup](#13-mongodb-setup)
14. [Bootstrap — `cmd/api/main.go`](#14-bootstrap)
15. [Testing](#15-testing)
16. [Docker](#16-docker)
17. [Folder-by-Folder Summary](#17-folder-by-folder-summary)
18. [Quick Reference Checklist](#18-quick-reference-checklist)

---

## 1. Project Structure

```
cmd/api/main.go                  # bootstrap: config → mongo → indexes → seed admin → router → graceful shutdown
internal/
├── config/config.go             # ALL env reads → Config struct. Nothing else calls os.Getenv.
├── database/mongo.go            # mongo.Client connect + ping; EnsureIndexes()
├── apperr/apperr.go             # AppError type + ERRORS catalog + From() helper
├── response/response.go         # Success / Paginated / Error envelopes; Page[T]
├── middleware/
│   ├── auth.go                  # Authenticate, RequireRole(...), Principal(c)
│   ├── error.go                 # Fail(c, err), ErrorHandler, Recovery, NotFound
│   ├── cors.go                  # CORS
│   ├── ratelimit.go             # per-IP token bucket
│   └── requestlog.go            # one structured line per request
├── model/
│   ├── user.go                  # User, UserView, Role, CreateUserInput, CollUsers
│   ├── screening.go             # Screening, ScreeningView, DocType, Verdict, ScreeningStatus, Decision, EngineResult
│   └── audit.go                 # AuditLog, action constants, CollAuditLogs
├── repository/
│   ├── pagination.go            # pageOf[DB, V] generic helper
│   ├── user_repo.go             # UserRepository interface + mongoUserRepo + constructor
│   ├── screening_repo.go
│   └── audit_repo.go            # Insert only — append-only by design
├── service/                     # business logic (this project's "controller" layer)
│   ├── auth_service.go          # login, refresh — bcrypt(12), JWT
│   ├── user_service.go          # create/list/profile, SeedAdmin
│   └── screening_service.go     # store image → call engine → persist → decide
├── screening/                   # THE external service boundary
│   ├── engine.go                # Engine interface + ScreenRequest / ScreenResult
│   ├── http_engine.go           # real: POST {SCREENING_SERVICE_URL}/api/v1/verify
│   └── mock_engine.go           # deterministic offline stub (SCREENING_ENGINE=mock)
├── storage/
│   ├── storage.go               # FileStore interface (Put/Get/Delete)
│   ├── local.go                 # local-disk impl (STORAGE_DRIVER=local, current default)
│   └── gridfs.go                # GridFS impl (STORAGE_DRIVER=gridfs) — same MongoDB
├── platform/jwt/jwt.go          # Manager: CreateAccess/Refresh, DecodeAccess/Refresh; TokenData
├── validate/validate.go         # BindJSON(c, dst) — the one place bodies are bound
├── transport/http/
│   ├── router.go                # Deps struct, NewRouter(), middleware order, route table, /health
│   ├── helpers.go               # pageParams(c)
│   ├── auth_handler.go
│   ├── user_handler.go
│   └── screening_handler.go
└── testsupport/mongo.go         # RequireMongo(t) — real, uniquely-named test DB per test
pkg/logger/logger.go             # slog JSON logger factory
```

**Layer names** map from a typical MVC backend as: `controller` → **`service`**,
`routes` → **`transport/http`** handlers, and there is no separate "types" folder —
domain types live in `model/`, cross-cutting ones in the package that owns them.

**Import direction (never violate):**
`transport/http` → `service` → `repository` → `model`.
`service` also → `screening`, `storage`. `middleware` → `apperr`, `response`, `platform/jwt`.
`model` imports nothing from this project except the bson package. Repositories never
import `service`; services never import `gin`.

---

## 2. Technology Stack

| Module | Purpose |
|---|---|
| `github.com/gin-gonic/gin` | HTTP router + binding |
| `go.mongodb.org/mongo-driver/v2` | Official MongoDB driver (v2 API — `bson.ObjectID`, `mongo.Connect(opts)` with no ctx) |
| `github.com/golang-jwt/jwt/v5` | JWT signing / verification |
| `golang.org/x/crypto/bcrypt` | Password hashing — **cost 12** everywhere (`service.BcryptCost`) |
| `github.com/go-playground/validator/v10` | Struct validation (via Gin `binding:` tags) |
| `github.com/joho/godotenv` | Loads `.env.<env>.local` in development |
| `golang.org/x/time/rate` | Rate-limit token buckets |
| `log/slog` (stdlib) | Structured logging — no third-party logger |

Go module: `github.com/sih26/ps188-backend`. Everything is one module; no replace directives.

---

## 3. Environment & Config

### Strategy: `.env.<APP_ENV>.local` + real env vars

- **`.env.development.local`** (gitignored, matches `.env.*.local`) holds every secret
  and setting for local runs. `config.Load()` loads it via `godotenv` when `APP_ENV`
  is unset or `development`.
- **`.env.example`** is committed with placeholder values — the template.
- **`docker-compose.yml`** loads `.env.development.local` via `env_file:` and overrides
  **only** `MONGO_URI` (and `APP_ENV`) in its `environment:` block, so the container
  reaches Mongo by service name. No secrets in compose.

### `internal/config/config.go`

Every `os.Getenv` call in the codebase lives in this file. Everywhere else, take a
`*config.Config` (or the specific values) by parameter.

```go
cfg, err := config.Load()   // returns an error for missing required values (JWT_SECRET, ...)
```

Rules:
- Never read `os.Getenv` outside `config.go` (exception: `LOG_LEVEL` in `main` before
  config exists, and `TEST_MONGO_URI` in `testsupport`).
- Required-but-missing → `Load()` returns an error; the process exits. No silent zero values.
- Durations are parsed with `time.ParseDuration` (`JWT_ACCESS_TTL=1h`).

Key vars: `APP_ENV`, `PORT`, `MONGO_URI`, `MONGO_DB`, `JWT_SECRET`, `JWT_REFRESH_SECRET`,
`JWT_ACCESS_TTL`, `JWT_REFRESH_TTL`, `CORS_ALLOW_ORIGINS`, `MAX_UPLOAD_BYTES`,
`RATE_LIMIT_PER_MIN`, `ADMIN_USERNAME/PASSWORD/EMAIL`, `SCREENING_ENGINE` (`http|mock`),
`SCREENING_SERVICE_URL`, `SCREENING_SERVICE_API_KEY`, `SCREENING_SERVICE_TIMEOUT`.

---

## 4. The Error Convention

There is no `Result` type. This is idiomatic Go: functions return `(T, error)`. The
**rule** is that the `error` a service or repository returns is **always** an
`*apperr.AppError` drawn from the `ERRORS` catalog — never `errors.New`, never
`fmt.Errorf`, never an inline `&AppError{}`.

### `internal/apperr/apperr.go`

```go
type AppError struct {
    Message    string  // client-safe
    Code       int     // stable numeric id (see numbering)
    HTTPStatus int
    wrapped    error   // logs only, never serialised
}

func (e *AppError) Wrap(cause error) *AppError   // attach a cause for logs, keep code/message
func From(err error) *AppError                   // normalise anything → *AppError (unknown → UnhandledError)
```

### Numbering convention

```
1xxxx  common / general        4xxxx  screenings
2xxxx  auth & authorization    5xxxx  files / storage
3xxxx  users                   6xxxx  external screening engine
```

### Catalog — every error is a named field on `ERRORS`

```go
var ERRORS = struct{ /* ...named fields... */ }{
    DatabaseError:      def("Database operation failed", 10001, 500),
    ScreeningNotFound:  def("Screening not found", 40001, 404),
    AlreadyDecided:     def("This screening already has an officer decision", 40002, 409),
    // ...
}
```

### Usage

```go
// repository — convert the driver error at the boundary, once:
if errors.Is(err, mongo.ErrNoDocuments) {
    return nil, apperr.ERRORS.ScreeningNotFound
}
if err != nil {
    return nil, apperr.ERRORS.DatabaseError.Wrap(err)   // Wrap keeps the cause for logs
}

// service — propagate as-is, or translate to a domain-specific code:
sc, err := s.repo.FindByID(ctx, id)
if err != nil {
    return zero, err            // already an *AppError
}

// service — inspect a code without type juggling:
if ae := apperr.From(err); ae.Code == apperr.ERRORS.UserNotFound.Code {
    return nil, apperr.ERRORS.InvalidCredentials   // don't leak which usernames exist
}
```

Rules:
- Add every new error to `ERRORS` first, with a code in the right range. Reference it.
- `.Wrap(cause)` only at the boundary where a foreign error is first seen (driver,
  `json.Unmarshal`, `http.Client.Do`). Never wrap an `*AppError` in another.
- Handlers never build error responses — they call `middleware.Fail(c, err)` and return.

---

## 5. Response Envelopes

`internal/response/response.go`. Handlers call these; they never write `c.JSON` maps by hand.

```go
response.Success(c, http.StatusOK, data, "Screening")            // {success, message, data, timestamp}
response.Paginated(c, page, "Screenings")                        // + {page:{has_next, next_cursor}}
response.Error(c, apperr.From(err))                              // {success:false, error:{code,message}} — middleware calls this
```

Shapes:

```jsonc
// success
{ "success": true, "message": "Screening", "data": { ... }, "timestamp": "2026-09-01T..." }
// paginated
{ "success": true, "message": "Screenings", "data": [ ... ],
  "page": { "has_next": true, "next_cursor": "66d..." }, "timestamp": "..." }
// error
{ "success": false, "error": { "code": 40001, "message": "Screening not found" }, "timestamp": "..." }
```

Binary responses (the document image) bypass the envelope — the handler buffers the
blob and calls `c.Data(status, contentType, bytes)`. Errors still flow through the
JSON envelope.

---

## 6. Cursor Pagination

`_id`-based, newest-first. No `skip`/`offset`.

- Client sends `?cursor=<hex ObjectID>&limit=<n>` (`limit` clamped to `[1, 100]`, default 20).
- Repository filters `_id < cursor` (when present), sorts `{_id: -1}`, fetches `limit + 1`.
- `repository.pageOf(rows, limit, toView, idOf)` trims the sentinel row, maps to views,
  and sets `NextCursor` to the last row's id when `HasNext`.

```go
opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(limit + 1)
cur, err := r.coll.Find(ctx, filter, opts)
// ...cur.All(ctx, &rows)...
return pageOf(rows, limit,
    func(s model.Screening) model.ScreeningView { return s.View() },
    func(s model.Screening) string { return s.ID.Hex() }), nil
```

Repositories return `response.Page[V]`; handlers pass it straight to `response.Paginated`.

---

## 7. Models

`internal/model/<entity>.go`. Each entity file exports:

1. `Coll<Entity>` — the collection name constant.
2. The **stored struct** — `bson:"..."` tags, `ID bson.ObjectID \`bson:"_id,omitempty"\``.
3. The **`View` struct** — `json:"..."` tags, only client-safe fields, plus a
   `func (e *Entity) View() EntityView` mapper.
4. Domain enums as `type X string` with typed constants and a `Valid() bool` method.
5. Validated input structs (`CreateUserInput`) with Gin `binding:` tags.

```go
const CollUsers = "users"

type Role string
const ( RoleVerifier Role = "verifier"; RoleAdmin Role = "admin"; RoleSuperAdmin Role = "superadmin" )
func (r Role) Valid() bool { /* switch */ }

type User struct {
    ID           bson.ObjectID `bson:"_id,omitempty"`
    Username     string        `bson:"username"`
    PasswordHash string        `bson:"password_hash"`   // never in a View
    // ...
}

type UserView struct { ID string `json:"id"`; Username string `json:"username"`; /* ... */ }
func (u *User) View() UserView { return UserView{ ID: u.ID.Hex(), /* ... */ } }
```

Rules:
- Times are `time.Time`, always stored UTC (`time.Now().UTC()`), set in the repository.
- `View` structs never carry `PasswordHash` or internal bookkeeping fields.
- Enum values are lowercase snake for storage (`national_id`); `Verdict` is the one
  exception — it mirrors the engine's `UPPER_SNAKE` output verbatim.
- Input structs live in the model file, not the service or handler.

---

## 8. Repositories

`internal/repository/<entity>_repo.go`. The **only** layer that talks to MongoDB.
Zero business logic — queries and mapping only.

### Structure: interface + unexported mongo impl + constructor

```go
type ScreeningRepository interface {
    Create(ctx context.Context, s *model.Screening) (*model.Screening, error)
    FindByID(ctx context.Context, id string) (*model.Screening, error)
    List(ctx context.Context, f model.ScreeningFilter, cursor string, limit int64) (response.Page[model.ScreeningView], error)
    SetResult(ctx context.Context, id string, /* ... */) (*model.Screening, error)
    SetDecision(ctx context.Context, id string, d model.OfficerDecision) (*model.Screening, error)
    NextSequence(ctx context.Context, day string) (int64, error)
}

type mongoScreeningRepo struct { coll *mongo.Collection; counters *mongo.Collection }

func NewScreeningRepository(db *mongo.Database) ScreeningRepository {
    return &mongoScreeningRepo{ coll: db.Collection(model.CollScreenings), counters: db.Collection("counters") }
}
```

### Rules

- Every method takes `ctx context.Context` **first** and returns `(T, error)` where the
  error is an `*apperr.AppError`.
- `mongo.ErrNoDocuments` on a "get one" → `err(ERRORS.XNotFound)`. Never return `nil, nil`.
- A "find many" that matches nothing → `ok(empty page)`, not an error.
- Any other driver error → `ERRORS.DatabaseError.Wrap(err)`. `mongo.IsDuplicateKeyError`
  → `ERRORS.DuplicateResource.Wrap(err)`.
- Always parameterise by `bson.M` / `bson.D` — never build query strings.
- An invalid hex id passed to `FindByID` is treated as not-found (`ObjectIDFromHex` fails
  → return the not-found error), not a 500.
- Conditional writes enforce invariants in one atomic op: `SetDecision` filters on
  `officer_decision: {$exists: false}` so a second concurrent decision matches zero
  docs and comes back `mongo.ErrNoDocuments` — the service maps that to `AlreadyDecided`.
- `NextSequence` uses `FindOneAndUpdate` with `$inc` + upsert + `ReturnDocument(After)`
  for a gapless per-day counter (backs `reference_no`).
- Constructors are `New<Entity>Repository(db)` returning the **interface**. No global state.

---

## 9. Services

`internal/service/<name>_service.go`. All business logic: validation of business rules,
password hashing, token issuance, orchestration across repositories + the screening
engine + storage, and writing the audit log.

```go
type ScreeningService struct {
    repo   repository.ScreeningRepository
    audit  repository.AuditRepository
    files  storage.FileStore
    engine screening.Engine
    log    *slog.Logger
}
func NewScreeningService(/* deps */) *ScreeningService { /* ... */ }
```

### Rules

- Methods take `ctx` first, return `(SomethingView, error)` or `(response.Page[View], error)`.
- Never import `gin`. Never touch `*http.Request` / `*gin.Context`. Inputs are plain
  structs (`SubmitInput`) or scalars built by the handler.
- Map to `View` before returning — never hand a raw stored struct with `PasswordHash` up.
- Propagate repo errors with `if err != nil { return zero, err }`; translate to a
  domain code only when the situation demands it (`UserNotFound` → `InvalidCredentials`).
- The audit write is best-effort and never fails the operation:
  `_ = s.audit.Insert(ctx, model.AuditLog{ ... })`.
- bcrypt cost is always `service.BcryptCost` (12).
- An external-engine failure inside `Submit` is **persisted** (`status=failed`,
  `failure_reason=…`) and the case is still returned — the officer can act manually.
  Only truly fatal errors (storage down, DB down) propagate.
- `SeedAdmin` is idempotent: it creates the bootstrap admin only when
  `users.Count() == 0`, and is a no-op on every later boot.

---

## 10. Transport / HTTP Handlers

`internal/transport/http/`. Thin. Handlers: bind input → call one service method →
`response.Success` / `response.Paginated`, or `middleware.Fail(c, err); return`.

```go
func (h *screeningHandler) decide(c *gin.Context) {
    var body decisionBody
    if err := validate.BindJSON(c, &body); err != nil { middleware.Fail(c, err); return }

    actor, _ := middleware.Principal(c)
    view, err := h.screenings.Decide(c.Request.Context(), c.Param("id"),
        actor.UserID, c.ClientIP(), model.Decision(body.Decision), body.Reason)
    if err != nil { middleware.Fail(c, err); return }

    response.Success(c, http.StatusOK, view, "Decision recorded")
}
```

### Rules

- JSON bodies are bound **only** via `validate.BindJSON(c, &dst)` — it returns
  `InvalidRequestBody` for malformed JSON, `ValidationError` for tag violations.
- Request DTOs (`loginBody`, `decisionBody`) are unexported structs in the handler file
  with `binding:` tags. Anything richer belongs in `model` as a `CreateXInput`.
- Read the principal with `middleware.Principal(c)` (set by `Authenticate`).
- Path params: `c.Param("id")` is passed straight to the service, which asks the
  repository — an invalid id surfaces as the domain not-found error, no handler-side
  int parsing.
- `net/http` is imported as `nethttp` in handler files (the package itself is `http`).
- Pagination query parsing is `pageParams(c)` from `helpers.go`.
- `201` for resource creation, `200` otherwise.
- Multipart upload (screening submit): `c.FormFile("document")`, enforce
  `fh.Size <= maxUpload`, read with `io.LimitReader`, sniff type with
  `nethttp.DetectContentType` (accept `image/jpeg`, `image/png`), then hand bytes to
  the service.

### Router (`router.go`)

`NewRouter(Deps)` builds the engine. Middleware order is fixed:

```go
r.Use(
    middleware.Recovery(log),         // panic → 500 envelope
    middleware.ErrorHandler(log),     // c.Errors → error envelope (runs after handler)
    middleware.RequestLog(log),
    middleware.CORS(cfg.CORSAllowOrigins),
    middleware.RateLimit(cfg.RateLimitPerMin),
)
r.NoRoute(middleware.NotFound())
```

Route groups apply auth once: `api.Group("/screenings", middleware.Authenticate(jwt))`,
then per-route `middleware.RequireRole(...)`.

---

## 11. Middleware

`internal/middleware/`.

| File | Exports | Notes |
|---|---|---|
| `error.go` | `Fail(c, err)`, `ErrorHandler(log)`, `Recovery(log)`, `NotFound()` | `Fail` records the error + `c.Abort()`. `ErrorHandler` runs `c.Next()` then converts `c.Errors.Last()` via `apperr.From`; logs 5xx. |
| `auth.go` | `Authenticate(jwt)`, `RequireRole(...roles)`, `Principal(c)` | `Authenticate` requires `Authorization: Bearer <token>`, sets the principal. `RequireRole` must be chained **after** `Authenticate`. |
| `cors.go` | `CORS(origins)` | `*` allows any; otherwise exact `Origin` match. Short-circuits `OPTIONS`. |
| `ratelimit.go` | `RateLimit(perMinute)` | Per-`ClientIP` `golang.org/x/time/rate` bucket, idle entries evicted every 3 min. `0` disables. |
| `requestlog.go` | `RequestLog(log)` | One `slog` line after each request: method, path, status, duration, ip, user. |

Rules: middleware never contains business logic. It fails via `middleware.Fail(c, apperr.ERRORS.X)`.

---

## 12. The External Screening Engine

`internal/screening/`. This is the project's **only** external service boundary and
the **only** thing ever stubbed in a test.

```go
type Engine interface {
    Screen(ctx context.Context, req ScreenRequest) (*ScreenResult, error)
}
```

- **`http_engine.go`** — the real client. `POST {baseURL}/api/v1/verify` as
  `multipart/form-data` (`image`, `doc_type` ∈ `auto|passport|aadhaar`, `doc_number`,
  `mrz_line1`, `mrz_line2`), header `X-API-Key`. Response JSON: `{success, filename,
  doc_type, verdict, risk_score, reasons[], extracted_fields[]?, evidence[]?,
  evidence_table{}}` — served by `passport-model/server.py` (hosted at
  `https://passport-model.onrender.com`).
  Any transport error / non-200 / timeout → `ERRORS.ScreeningEngineUnavailable.Wrap(...)`
  (the model's `{error:{code,message}}` envelope is surfaced in the wrapped error).
  Unparseable body / empty verdict → `ERRORS.ScreeningEngineBadResponse`.
- **`mock_engine.go`** — `MockEngine{Force, Err}`. Deterministic verdict derived from a
  checksum of the image bytes; `Force` pins a verdict, `Err` simulates an outage.
  Selected with `SCREENING_ENGINE=mock`. Used for offline dev and as the test stub.
- `main` picks the impl from `cfg.ScreeningEngine`; everything downstream depends on the
  `Engine` **interface**.

`ScreenResult.RawEvidence` (`map[string]any`) is stored verbatim as `engine.evidence`
— full explainability, never reshaped. `ExtractedFields []ExtractedField` (per-field
OCR confidence) and `EvidenceItems []EvidenceItem` (`good|warn|bad` tone) are the
structured forms the UI reads; `DeriveEvidence(reasons, risk)` fills the toned list
when the model omits it. `risk_score` is stored `0.0–1.0` and converted to an integer
`0–100` by `model.riskTo100` in every API projection (`ScreeningView`, `EngineView`).

---

## 13. MongoDB Setup

`internal/database/mongo.go`.

```go
client, db, err := database.Connect(ctx, cfg.MongoURI, cfg.MongoDB)   // dials + pings; caller Disconnects
err = database.EnsureIndexes(ctx, db)                                 // idempotent, run on every boot
```

- `mongo-driver/v2`: `mongo.Connect(opts)` takes **no context**; `client.Ping(ctx, readpref.Primary())`.
- ObjectIDs are `bson.ObjectID` (`bson.NewObjectID()`, `bson.ObjectIDFromHex()`) — **not**
  `primitive.ObjectID` (that's v1).
- `EnsureIndexes` is the single place indexes are declared — `coll.Indexes().CreateMany(ctx, []mongo.IndexModel{...})`,
  each with `options.Index().SetName(...)`. See `database-design.md` §Indexes for the list.
- Document images: `STORAGE_DRIVER=local` (default) writes plain files under
  `LOCAL_STORAGE_DIR`; `STORAGE_DRIVER=gridfs` keeps them in **GridFS**
  (`db.GridFSBucket()`, `storage.NewGridFS(db)`) instead — same database, no shared
  filesystem volume, survives multi-replica. Both hand out `bson.ObjectID` hex ids,
  so `Screening.ImageFileID` and everything downstream is unaffected by which one
  is wired in `main.go`.
- No ORM, no migration tool. Schema is enforced by the Go structs + these indexes.

---

## 14. Bootstrap

`cmd/api/main.go` — `run(log)` does, in order:

1. `config.Load()`
2. `database.Connect()` (+ `defer client.Disconnect`)
3. `database.EnsureIndexes()`
4. construct repositories → screening engine (http|mock) → jwt manager → services
5. `userSvc.SeedAdmin(...)` — first run only
6. `httptransport.NewRouter(Deps{...})`
7. `http.Server{ReadHeaderTimeout: 10s}` in a goroutine; `signal.Notify` on
   `SIGINT`/`SIGTERM` → `srv.Shutdown(15s ctx)`.

`main()` itself only builds the logger and calls `run`, exiting non-zero on error.

---

## 15. Testing

**Philosophy (inherited): real dependencies, no repository mocks.** Service and
repository tests run against a **real MongoDB** — the one from `docker compose up`
(`TEST_MONGO_URI`, default `mongodb://localhost:27017`). The **only** stubbed boundary
is the external screening engine.

### `internal/testsupport/mongo.go`

```go
db := testsupport.RequireMongo(t)   // fresh uniquely-named DB, indexes created, dropped in t.Cleanup;
                                    // t.Skip (not Fail) if no MongoDB is reachable
```

### Repository tests — `*_repo_test.go`, package `repository_test`

Call the repository directly. Assert on the returned value/error, then verify the row.

```go
func TestScreeningRepository_SetDecision_SingleWriter(t *testing.T) {
    db := testsupport.RequireMongo(t)
    repo := repository.NewScreeningRepository(db)
    // first decision ok; second → mongo.ErrNoDocuments (conditional write)
}
```

### Service tests — `*_service_test.go`, package `service_test`

Real repositories + a real `FileStore` (local, rooted at `t.TempDir()` — self-cleans)
+ `screening.MockEngine`. Assert on outcomes and the persisted state, not on which
internal calls happened.

```go
files, _ := storage.NewLocalFileStore(t.TempDir())
svc := service.NewScreeningService(
    repository.NewScreeningRepository(db), repository.NewAuditRepository(db),
    files, &screening.MockEngine{Force: model.VerdictSuspicious}, blacklistSvc,
    slog.New(slog.NewTextHandler(io.Discard, nil)),
)
```

### The one stub — `internal/screening/http_engine_test.go`

`httptest.Server` returning canned `/api/v1/verify` JSON. This is the **only** place a
dependency is faked. Everything else (DB, repositories, other services) stays real.

### Rules

- One `testsupport.RequireMongo(t)` call per test (a fresh DB); it self-cleans.
- Never `t.Fatal` for a missing MongoDB — `RequireMongo` already `t.Skip`s.
- Test the important branches: success + side effects, each guard, not-found, wrong-state,
  the conditional-write race.
- No mocking of this project's own repositories or services — ever.
- On Windows, run tests with `GOTMPDIR` inside the repo (`make test` does this) so Smart
  App Control does not block the compiled test binaries in `%TEMP%`.

---

## 16. Docker

### `Dockerfile` — multi-stage

```dockerfile
FROM golang:1.26 AS build
# go mod download (cached) → CGO_ENABLED=0 build → /out/api
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /app/api
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
```

Static binary, distroless, non-root. `.dockerignore` excludes `docs`, `.env*`, `.git`.

### `docker-compose.yml`

- **`mongo`** (`mongo:7`) — named volume `ps188_mongo_data`, `docker/mongo-init.js`
  mounted to `/docker-entrypoint-initdb.d/` (creates the `ps188_app` user on first boot),
  `mongosh` ping healthcheck, JSON log rotation.
- **`backend`** — `build: .`, `depends_on: mongo: condition: service_healthy`,
  `env_file: .env.development.local`, `environment:` overrides **only** `MONGO_URI`
  (→ `mongo` service name) and `APP_ENV`.
- No secrets in the compose file. `SCREENING_ENGINE=mock` by default so the stack runs
  with no external model; set `SCREENING_ENGINE=http` + `SCREENING_SERVICE_URL` to point
  at the hosted FastAPI model.

`make up` / `make down` / `make logs`.

---

## 17. Folder-by-Folder Summary

| Folder / file | Contains | Never contains |
|---|---|---|
| `internal/config` | all env reads, `Config` | business logic, DB access |
| `internal/database` | connect, `EnsureIndexes` | queries, business logic |
| `internal/apperr` | `AppError`, `ERRORS` catalog | anything else |
| `internal/response` | JSON envelopes, `Page[T]` | business logic |
| `internal/model` | stored structs, `View`s, enums, input DTOs | queries, HTTP, business rules |
| `internal/repository` | all Mongo queries, interfaces + impls | business rules, JWT, bcrypt, HTTP |
| `internal/service` | business logic, orchestration, audit writes | `gin`, SQL/Mongo queries, `req`/`res` |
| `internal/screening` | external model client (iface + http + mock) | DB, HTTP server code |
| `internal/storage` | `FileStore` iface + local-disk / GridFS impls | business logic |
| `internal/middleware` | reusable Gin middleware | business logic |
| `internal/transport/http` | handlers, router, route table | SQL/Mongo, bcrypt, JWT signing |
| `internal/platform/jwt` | token create/verify, `TokenData` | DB, HTTP |
| `internal/validate` | `BindJSON` | domain logic |
| `internal/testsupport` | `RequireMongo` | production code paths |
| `pkg/logger` | slog factory | anything domain-specific |
| `cmd/api` | wiring + lifecycle only | business logic |

---

## 18. Quick Reference Checklist

**Model**
- [ ] `Coll<Entity>` constant · stored struct with `bson` tags · `View` struct with `json` tags · `View()` mapper
- [ ] Enums are `type X string` + typed constants + `Valid() bool`
- [ ] No secret fields in the `View`

**Repository**
- [ ] `type XRepository interface` + unexported mongo impl + `NewXRepository(db) XRepository`
- [ ] Every method: `ctx` first, returns `(T, error)` that is always an `*apperr.AppError`
- [ ] `mongo.ErrNoDocuments` → `ERRORS.XNotFound`; other driver errors → `ERRORS.DatabaseError.Wrap(err)`
- [ ] duplicate key → `ERRORS.DuplicateResource.Wrap(err)`
- [ ] find-many-empty → empty page, not an error
- [ ] invariants enforced by conditional writes, not read-then-write
- [ ] no business logic

**Service**
- [ ] takes deps via constructor; never imports `gin`
- [ ] maps to `View` before returning
- [ ] propagates repo errors; translates codes only where justified
- [ ] audit write is `_ = s.audit.Insert(...)` (best-effort)
- [ ] bcrypt cost = `BcryptCost`

**Handler**
- [ ] binds via `validate.BindJSON`
- [ ] one service call, then `response.Success` / `Paginated` or `middleware.Fail(c, err); return`
- [ ] `middleware.Principal(c)` for the actor
- [ ] `201` on create, `200` otherwise
- [ ] registered in `router.go` with the right `RequireRole`

**Error catalog**
- [ ] new error added to `ERRORS` with a code in the correct `Nxxxx` range

**Tests**
- [ ] `testsupport.RequireMongo(t)` (real DB, self-cleaning)
- [ ] no mocks except `screening.MockEngine` / `httptest` for the engine
- [ ] success + each guard branch + not-found + wrong-state + conditional-write race
