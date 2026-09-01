# PS188 Backend — TODO

## Project setup
- [x] Initialize Go module (`github.com/sih26/ps188-backend`)
- [x] Add dependencies (Gin, mongo-driver/v2, jwt/v5, bcrypt, validator, godotenv, uuid, x/time/rate)
- [x] `.gitignore`, `.gitattributes`, `.dockerignore`
- [x] `Makefile` (run / build / test / tidy / fmt / vet / up / down / logs)
- [x] `.env.example` and `.env.development.local`
- [x] Top-level `README.md`

## Core / infrastructure
- [x] `internal/config` — single place for all env reads → `Config`
- [x] `internal/apperr` — `AppError` type + `ERRORS` catalog + `From()` helper + numbering convention
- [x] `internal/response` — Success / Paginated / Error JSON envelopes + `Page[T]`
- [x] `pkg/logger` — slog JSON logger factory
- [x] `internal/platform/jwt` — token `Manager` (access + refresh), `TokenData`
- [x] `internal/database` — Mongo connect + ping, `EnsureIndexes()` (idempotent)
- [x] `internal/validate` — `BindJSON` request-body binding helper

## Models
- [x] `model/user.go` — `User`, `UserView`, `Role`, `UserStatus`, `CreateUserInput`
- [x] `model/screening.go` — `Screening`, `ScreeningView`, `DocType`, `Verdict`, `ScreeningStatus`, `Decision`, `EngineResult`, `OfficerDecision`, `ScreeningFilter`
- [x] `model/audit.go` — `AuditLog` + action constants

## Repositories (MongoDB)
- [x] `repository/pagination.go` — generic `pageOf[DB, V]` cursor helper
- [x] `repository/user_repo.go` — interface + mongo impl (create, find by id/username, list, count)
- [x] `repository/screening_repo.go` — create, find, list+filters, `SetResult`, `SetDecision` (conditional single-writer), `NextSequence` (atomic per-day counter)
- [x] `repository/audit_repo.go` — Insert only (append-only)

## External screening engine
- [x] `screening/engine.go` — `Engine` interface + `ScreenRequest` / `ScreenResult`
- [x] `screening/http_engine.go` — real client: `POST {SCREENING_SERVICE_URL}/predict` (multipart + X-API-Key), maps to `SCREENING_ENGINE_UNAVAILABLE` / `SCREENING_ENGINE_BAD_RESPONSE`
- [x] `screening/mock_engine.go` — deterministic offline stub (`SCREENING_ENGINE=mock`)

## Storage
- [x] `storage/storage.go` — `FileStore` interface (Put / Get / Delete)
- [x] `storage/gridfs.go` — GridFS implementation (document images in the same MongoDB)

## Services (business logic)
- [x] `service/auth_service.go` — login (bcrypt 12, indistinguishable unknown-user), refresh
- [x] `service/user_service.go` — create (dup guards), list, profile, `SeedAdmin` (idempotent)
- [x] `service/screening_service.go` — Submit (store image → allocate ref → call engine → persist result/failure), Get, List, StreamImage, Decide (one per screening) + audit logging

## Middleware
- [x] `middleware/error.go` — `Fail`, `ErrorHandler`, `Recovery`, `NotFound`
- [x] `middleware/auth.go` — `Authenticate`, `RequireRole(...)`, `Principal`
- [x] `middleware/cors.go` — configurable CORS
- [x] `middleware/ratelimit.go` — per-IP token bucket with idle eviction
- [x] `middleware/requestlog.go` — one structured line per request

## Transport / HTTP
- [x] `transport/http/router.go` — `Deps`, `NewRouter`, middleware order, route table, `/health`
- [x] `transport/http/helpers.go` — `pageParams`
- [x] `transport/http/auth_handler.go` — login, refresh-token
- [x] `transport/http/user_handler.go` — create, list, profile
- [x] `transport/http/screening_handler.go` — submit (multipart + type/size checks), list, get, image, decision

## Bootstrap
- [x] `cmd/api/main.go` — config → Mongo → indexes → build deps → seed admin → router → HTTP server with graceful shutdown (SIGINT/SIGTERM)

## Docker
- [x] `Dockerfile` — multi-stage, `CGO_ENABLED=0`, distroless non-root
- [x] `docker-compose.yml` — `mongo:7` (healthcheck, named volume, init script) + backend (`depends_on: service_healthy`, `env_file`, MONGO_URI override only)
- [x] `docker/mongo-init.js` — creates the scoped `ps188_app` DB user on first boot

## Tests
- [x] `internal/testsupport/mongo.go` — `RequireMongo(t)` (real, uniquely-named DB per test, self-cleaning, skip if no Mongo)
- [x] `repository/screening_repo_test.go` — create/find, not-found mapping, pagination across pages, `SetDecision` single-writer race, `NextSequence` monotonicity
- [x] `service/screening_service_test.go` — submit→completed, engine-down→failed persisted, invalid doc type, decide-once→then `ALREADY_DECIDED`, invalid decision
- [x] `service/auth_service_test.go` — login success / wrong password / unknown user indistinguishable / refresh
- [x] `screening/http_engine_test.go` — httptest stub: happy path + evidence passthrough, non-200 → unavailable, bad JSON → bad-response, API-key header

## Documentation (rewritten for PS188 — Go + MongoDB, RBAC, external model)
- [x] `docs/backend/BACKEND_GUIDE.md` — Go code style & architecture bible
- [x] `docs/backend/backend-architecture.md` — module map, endpoints, screening lifecycle, error codes, future modules, build order
- [x] `docs/backend/database-design.md` — MongoDB collections, document shapes, indexes, design decisions

## Role model change — collapse to two roles
- [x] Merge `officer` into `supervisor` — `Role` enum is now `supervisor` + `admin` only
- [x] `model/user.go` — remove `RoleOfficer`, update `Role.Valid()`
- [x] `transport/http/router.go` — screening submit/decision now require `supervisor`
- [x] Update tests to the two-role model
- [x] Update `docs/backend/*.md` + `README.md` (role tables, examples, `users` doc)

## Verification
- [x] `go build ./...` — compiles
- [x] `go vet ./...` — clean
- [x] `gofmt -l .` — clean
- [x] `go test ./...` — all packages green against a real local MongoDB
- [x] `CGO_ENABLED=0 GOOS=linux` cross-compile — validates the Dockerfile build step
- [x] `docker compose config` — valid

---

## Pending — not yet built

### Blacklisting  (PS: "expired or blacklisted travel documents", "multiple identities")
- [ ] `blacklist` collection — blacklisted document numbers + identities (name/DOB/nationality), `reason`, `source`, `added_by`, `active`, timestamps; indexes on `doc_number` and identity fields
- [ ] `model/blacklist.go` + `BlacklistRepository` (create, list/paginate, lookup by doc number, lookup by identity, deactivate)
- [ ] `BlacklistService` — admin CRUD + a `Check(docNumber, fields)` used during screening submit
- [ ] Wire into `ScreeningService.Submit` — on a hit: raise `blacklist_hit` flag on the screening, bump `risk_score`, add a reason; never auto-block (officer still decides)
- [ ] Expiry check — flag documents whose extracted/entered expiry date is past (`expired_document` flag)
- [ ] `screenings` doc: add `flags []string` (e.g. `blacklist_hit`, `expired_document`, `multiple_identity`) + `blacklist_matches []{...}`
- [ ] Endpoints — `POST/GET/PATCH /api/blacklist` (admin), `GET /api/blacklist/check` (supervisor)
- [ ] Error domain `7xxxx` — `BLACKLIST_ENTRY_NOT_FOUND`, `BLACKLIST_ENTRY_EXISTS`
- [ ] Audit actions — `blacklist.added`, `blacklist.updated`, `blacklist.removed`
- [ ] Tests — repo (lookup by doc number / identity), service (`Check` hit/miss), submit-flow integration (hit → flag + risk bump)
- [ ] Docs — promote `backend-architecture.md` §8 "Blacklist" from sketch to a full module section; add the `blacklist` collection to `database-design.md`

### Other future modules (sketched in `backend-architecture.md` §8, not started)
- [ ] Checkpoints registry (`/api/checkpoints`)
- [ ] Face verification module (PS Module 4) behind a `FaceEngine` interface
- [ ] Cases — link multiple screenings of the same traveller (multiple-identity detection)
- [ ] Audit read API (`GET /api/audit-logs`, admin)
- [ ] Dashboard summary (`GET /api/dashboard/summary`)
- [ ] Async screening (worker + `202 processing`) if the model gets slow
