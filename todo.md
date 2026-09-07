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
- [x] `storage/local.go` (2026-09-04) — local-disk `FileStore`; mints a
      `bson.ObjectID` hex id (same shape GridFS handed out, so `Screening.ImageFileID`
      round-trips unchanged) and writes to `<LOCAL_STORAGE_DIR>/<id>`. `STORAGE_DRIVER`
      config (`local` default | `gridfs`) selects the impl in `main.go`. `.env.*`,
      `.gitignore` (`/data/`), `.dockerignore` updated. `Dockerfile` creates
      `/app/data/uploads` `chown`ed to the distroless `nonroot` user (no shell at
      runtime to mkdir); `docker-compose.yml` mounts a named volume
      (`ps188_uploads:/app/data/uploads`) so uploads survive restarts. Service tests
      (`screening_service_test.go`) switched from GridFS to a `t.TempDir()`-rooted
      local store — now exercise the production-default path with automatic cleanup.
      New `storage/local_test.go` (put/get/delete round-trip, id is a valid ObjectID
      hex, delete-missing is a no-op, base dir auto-created).
      Docs — `BACKEND_GUIDE.md` §storage/§13/testing, `backend-architecture.md` §3/§9,
      `database-design.md` §1/§3/§7/§9, `README.md`.

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

# Hackathon priority  *(2026-09-03 — internal demo, main requirements first)*

Blacklist + expiry wiring is **done**. OCR lives in `passport-model/`, not
here. Priority order below is for the demo narrative: verifier submits a doc → sees
risk / verdict / evidence / flags → decides; admin/superadmin see dashboards, flagged
cases, audit trail.

**Commit the uncommitted Phase A / Phase C / blacklist-wiring work before starting.**

## P0 — demo blockers (do next)
- [ ] **Phase K contract fixes** *(XS)* — ~~`risk_score` as int `0–100`~~ **done in Phase D**;
      still to do: add `http://localhost:5173` to `CORS_ALLOW_ORIGINS`; audit
      `ScreeningView` fields against what the UI reads.
- [x] **Audit read API — Phase G core** *(done 2026-09-07)* — `AuditRepository.List`
      (repo stays insert-only otherwise, append-only) + `GET /api/audit-logs`, scoped by
      `middleware.ScopeAuditToActor` (verifier = own `user_id`, admin = own region
      fail-closed if unset, superadmin = org-wide), cursor-paginated newest first.
      `AuditLog`/`AuditLogView` gained `Region` (denormalised at write time). `/users`
      list also gained region scoping (`UserFilter`) in the same commit. Frontend
      `AuditLog.jsx`/`AuditTrail.jsx` wired to it via `features/audit/` — only the
      `SuperAdminDashboard.jsx` audit card still reads mock data (frontend-only, no
      backend work left).
- [ ] **Dashboard summary — Phase E minimal** *(M)* — `GET /api/dashboard/summary`,
      role-aware: screenings today, verdict split, pending decisions, weekly volume.
      **Skip "accuracy %"** until its definition is signed off.

## P1 — strong demo value
- [ ] **Phase H — user management** *(M)* — `GET /api/users?role=&region=&status=`
      (admin auto-scoped to `verifier` + own region; superadmin lists `admin`) +
      `PATCH /api/users/:id` (enable/disable, reassign `checkpoint_id`/`region`;
      role change superadmin-only). Audit `user.updated` / `user.disabled`.
- [ ] **Cases / multiple-identity detection** *(M)* — link screenings sharing an
      identity (name+DOB, or same `doc_number` across different holders) → raise the
      existing `multiple_identity` flag. Named PS requirement.
- [x] **Phase D — backend-only parts** *(done 2026-09-04)* — `risk_score` 0–100 via
      `model.riskTo100`; `INSUFFICIENT_IMAGE_QUALITY` kept + `verdict_band` added +
      flagged for frontend; structured `extracted_fields[]` / `evidence[]` with a
      derivation fallback. See Phase D section below.

## P2 — if time allows
- [ ] **Face verification — Phase F** — `FaceEngine` iface + `MockFaceEngine` +
      `POST /api/screenings/:id/face`. Ship mock-only if the face teammates' service
      isn't ready; wire the real `httpFaceEngine` when it is.
- [ ] **`GET /api/reports` — Phase E** — doc-type / by-checkpoint breakdowns,
      fake-detection rate, avg decision time. Summary (P0) already covers the demo.
- [x] **Phase D — per-field confidence + evidence tone** *(done 2026-09-04)* — types
      landed (`[]ExtractedField`, `[]EvidenceItem`), backend consumes them when the
      model sends them and derives the tone otherwise. Only the `passport-model` side
      actually emitting them is still open (see Phase D deferred).

## P3 — post-hackathon
- [ ] Presence / online status (Phase I) — last-login stopgap is fine for the demo.
- [ ] Settings (Phase J) — mostly static policy.
- [ ] Async screening (worker + `202`).
- [ ] Port ICAO 9303 + Verhoeff checksums to Go (Python stays the oracle).
- [ ] `doc_number_hash` at rest; split model into separate deployed services.

## Continuous
- [ ] `go build ./... && go vet ./... && gofmt -l . && go test ./...` on real Mongo
      before every commit.
- [ ] Keep `.env.example` + `docs/backend/*.md` + `README.md` + memory current.

---

## Pending — not yet built

### Blacklisting  (PS: "expired or blacklisted travel documents", "multiple identities")
- [x] `blacklist` collection — blacklisted document numbers + identities (name/DOB/nationality), `reason`, `source`, `added_by`, `active`, timestamps; indexes on `doc_number`, identity fields, `kind`
- [x] `model/blacklist.go` + `BlacklistRepository` (create, find, list/paginate+filters, lookup by doc number, lookup by identity, deactivate)
- [x] `BlacklistService` — admin CRUD + deactivate + `Check(probe)` → `{hit, matches[]}` (dedupes doc + identity hits)
- [x] Endpoints — `GET /api/blacklist/check` (any authed), `POST/GET /api/blacklist` + `GET /api/blacklist/:id` + `POST /api/blacklist/:id/deactivate` (admin + superadmin)
- [x] Error domain `7xxxx` — `BLACKLIST_ENTRY_NOT_FOUND` 70001, `BLACKLIST_ENTRY_EXISTS` 70002, `INVALID_BLACKLIST_KIND` 70003, `BLACKLIST_FIELDS_MISSING` 70004
- [x] Audit actions — `blacklist.added`, `blacklist.deactivated`
- [x] Tests — repo (lookup by doc number / identity, case-insensitive, active-only, pagination), service (add → check hit, duplicate rejected, validation, identity check, deactivate clears hit)
- [x] Docs — `backend-architecture.md` §4.4 full module section, `blacklist` collection added to `database-design.md`
- [x] Wire into `ScreeningService.Submit` (2026-09-03) — after the engine call,
      `postEngineChecks` runs `BlacklistService.Check` on the submitted/extracted
      document number + identity. On a hit: `blacklist_hit` flag, `blacklist_matches[]`
      persisted, `engine.reasons` note appended, `risk_score += 0.25` (clamped). Never
      auto-blocks. `SubmitInput` gained `HolderName`/`DOB`/`Nationality`/`ExpiryDate`
      (form fields `holder_name`/`dob`/`nationality`/`expiry_date`); `checkpoint_id`/
      `region` still from the token. New repo method `ScreeningRepository.SetChecks`.
- [x] Expiry check (2026-09-03) — `parseExpiry` (ISO / DMY / MRZ YYMMDD forms); a past
      date raises `expired_document` and bumps `risk_score += 0.15` (clamped).
- [x] `screenings` doc: `flags []string` + `blacklist_matches []BlacklistMatch`
      (`{entry_id, kind, doc_number, name, reason, source}`); both surfaced in
      `ScreeningView` (always emitted, never `null`). `model.Flag*` constants
      (`blacklist_hit`, `expired_document`, `multiple_identity`, `face_mismatch`).
- [x] Tests — `screening_service_test.go` (blacklist hit → flag + match + risk bump +
      reason + persisted; clean → no flag, risk unchanged; expired date → flag, future
      date → none); `screening_repo_test.go` `SetChecks` (flags/matches/risk/reason
      append, verdict untouched). *(mongo-backed — skip locally, no Mongo/Docker.)*
- [x] Docs — `backend-architecture.md` §4.3/§4.4/§5, `database-design.md` §3.

### Other future modules (sketched in `backend-architecture.md` §8, not started)
- [x] Checkpoints registry (`/api/checkpoints`) — done in Phase A
- [ ] Face verification module (PS Module 4) behind a `FaceEngine` interface
- [ ] Cases — link multiple screenings of the same traveller (multiple-identity detection)
- [x] Audit read API (`GET /api/audit-logs`, scoped per role) — done 2026-09-07
- [ ] Dashboard summary (`GET /api/dashboard/summary`)
- [ ] Async screening (worker + `202 processing`) if the model gets slow

---

# Build plan — align backend to `ps188-UI-Design/`

Gap analysis of the current backend against the finished UI design (React app in
`ps188-UI-Design/`). Decisions taken 2026-09-02:

- **3 roles + regions** — `verifier` / `admin` (region-scoped) / `superadmin` (org-wide).
  This supersedes the "two roles only" note in the earlier checklist and in
  `backend-architecture.md` §2.
- **Decision vocabulary aligns to the UI** — `accept` / `escalate` / `reject`
  (replaces `clear` / `refer` / `detain`). **Done** — see Phase B.

Phases are ordered by dependency. A–C unblock everything; do them first.

## Phase A — Role model, regions, checkpoints, auth  *(foundational)*
- [x] `model/user.go` — `Role` enum → `RoleVerifier`, `RoleAdmin`, `RoleSuperAdmin`;
      `Role.Valid()` + `Role.AtLeastAdmin()`. `RoleSupervisor` renamed everywhere.
- [x] `middleware.RequireRole(admin, superadmin)` on account + blacklist routes;
      `verifier` on screening submit/decision.
- [x] `SeedAdmin` now seeds a `superadmin` (`SUPERADMIN_*` env, legacy `ADMIN_*` fallback).
- [x] Docs + tests updated to the 3-role model.
- [x] `model/user.go` — add `Region string` (admin + verifier) and `CheckpointID string`
      (verifier). `UserView` exposes both. Superadmin has empty region = all.
- [x] `CreateUserInput` — add `region` (required for admin), `checkpoint_id` (required
      for verifier); validate `checkpoint_id`/`region` against the checkpoints registry.
      (`MISSING_SCOPE_FIELD` 30006, `UNKNOWN_REGION` 80004.)
- [x] `model/checkpoint.go` — `Checkpoint` { `code` (e.g. `CP-04`), `region`, `admin_id`,
      `status` active|attention, timestamps }, `CheckpointView`.
- [x] `repository/checkpoint_repo.go` — create, list (filter by region/admin), find by code
      (case-insensitive), update (assign admin / status), `RegionExists`. Unique index on
      `code`, index on `region` + `admin_id`.
- [x] `service/checkpoint_service.go` — superadmin CRUD; `Resolve(code) → region` +
      `RegionExists(region)` used by user create (screening submit wiring is Phase C).
- [x] `transport/http` — `POST /api/checkpoints` (superadmin), `GET /api/checkpoints`
      (admin = own region, superadmin = all), `GET /api/checkpoints/:code` (admin),
      `PATCH /api/checkpoints/:code` (superadmin). Audit `checkpoint.created` /
      `checkpoint.updated`.
- [x] `middleware/auth.go` — `RequireRole` for the 3 roles (unchanged); `Principal` now
      carries `Region` / `CheckpointID` via `TokenData`. Helper `RegionScope(p)` (own
      region for admin/verifier, empty for superadmin) — the full `ScopeToActor(c, filter)`
      for screening lists lands in Phase C.
- [x] `platform/jwt` — `TokenData` gains `Region` + `CheckpointID`. Re-issued on refresh
      from the current user record.
- [x] Auth login by **email or username** — `loginBody` accepts `identifier` / `email` /
      `username`; `UserRepository.FindByIdentifier` matches either. Unknown-user /
      wrong-password indistinguishability kept.
- [x] `auth_service.Login` — writes an `auth.login` audit entry on success (actor, IP,
      region). New `ActionAuthLogin` constant. `Login` now takes an `ip` arg.
- [x] `POST /api/users/change-password` — `{current_password, new_password}`, any authed
      user, bcrypt-verify current. `INVALID_CURRENT_PASSWORD` 30005. Audit
      `user.password_changed`.
- [x] `POST /api/users/:id/reset-password` — admin resets a verifier in their region,
      superadmin resets anyone. Returns `{temp_password}`. Audit `user.password_reset`.
- [x] `cmd/api/main.go` — seeds a bootstrap **superadmin** (already the role). Config
      reads `SUPERADMIN_*` and falls back to legacy `ADMIN_*`. `.env.*` updated.
- [x] Tests — role/status validity (`model/role_test.go`), checkpoint repo + service CRUD,
      `Resolve`/`RegionExists`, verifier region resolution, admin region validation,
      email login, change/reset password + scope, `auth.login` audit. *(Written; run
      against a real Mongo — local Mongo/Docker were both down at commit time, so not yet
      executed here.)*
- [x] Docs — `backend-architecture.md` §2/§4.1/§4.2/§4.6/§5/§7/§8, `database-design.md`
      §1/§2/§2b/§5/§8/§9, `README.md` updated for regions, checkpoints, the new
      endpoints, `SUPERADMIN_*`, and the `8xxxx` error domain.

## Phase B — Decision vocabulary  *(done 2026-09-02)*
- [x] `model/screening.go` — `Decision` enum → `DecisionAccept`, `DecisionEscalate`,
      `DecisionReject`; `Decision.Valid()` updated.
- [x] `service/screening_service.go` — `screening.decided` audit `new_data.detail` =
      `screening.decided · ACCEPT|ESCALATE|REJECT`. Handler unchanged (validates via
      `Decision.Valid()` → `INVALID_DECISION`).
- [x] `apperr` — `INVALID_DECISION` message → "Decision must be one of accept, escalate, reject".
- [x] `screening_service_test.go` + `screening_repo_test.go` decision cases updated.
- [x] Docs — `backend-architecture.md` §4.3 / §5, `database-design.md` §3 / §5, BACKEND_GUIDE.

## Phase C — Screening scoping, filters, denormalised region
- [x] `model/screening.go` — added `Region string` (denormalised from the officer's
      checkpoint at submit time) and `Flags []string` (advisory markers; `ScreeningView`
      always emits `flags` as `[]`). Index `region + created_at`, and `officer_id` bumped
      to `officer_id + created_at`.
- [x] `ScreeningFilter` — added `OfficerID`, `Region`, `Decided *bool` (undecided filter),
      `DecisionValue`.
- [x] `screening_repo.List` — applies all the new filters (`officer_decision` `$exists`
      for `Decided`, `officer_decision.decision` for `DecisionValue`).
- [x] `middleware.ScopeToActor(c, *ScreeningFilter)` + `screening_handler.list` — verifier
      → own `officer_id` (UI "My History"); admin → own `region`; superadmin → unscoped.
      Honours `?decided=false`, `?decision=`, `?verdict=`, `?doc_type=`, `?status=`,
      `?checkpoint_id=`. Scoping overrides any client-supplied `officer_id`/`region`.
- [x] "Flagged for review" (UI admin dashboard) = `?verdict=SUSPICIOUS|FAKE&decided=false`
      through the scoped list endpoint — no new route.
- [x] `screening_service.Submit` — `SubmitInput.Region`; handler fills `checkpoint_id`
      + `region` from the verifier's token (Phase A stamped them on the JWT), no per-call
      user lookup. `screening.submitted` audit `new_data.region` added.
- [x] Tests — `screening_repo_test.go` list filters (officer / region / undecided /
      decision-value / flagged-for-review shape); `screening_service_test.go` region
      stamping + region list; `middleware/auth_test.go` `ScopeToActor` per role +
      `RegionScope`. *(mongo-backed tests skip locally — no Mongo/Docker; pure tests pass.)*
- [x] Docs — `backend-architecture.md` §4.3, `database-design.md` §3 (doc shape,
      `region`/`flags`, indexes, scoping note).

## Phase D — OCR fields, evidence tone, risk scale  *(done 2026-09-04)*
- [x] Per-field confidence — `EngineResult.ExtractedFields` is now
      `[]model.ExtractedField{ Label, Value, Confidence float64 }`. `http_engine`
      `parseExtractedFields` accepts the structured array **or** a flat `{label:value}`
      map (sorted by label); absent → nil, raw `evidence_table` always kept as
      `EngineResult.RawEvidence` (`json:"raw_evidence"`). `mock_engine` emits structured
      fields. `screening/engine.go` `ScreenResult` updated; `screening_service` blacklist/
      expiry checks read fields via `extractedValue(fields, labels…)`.
- [x] Evidence tone — `EngineResult.Evidence []model.EvidenceItem{ Tone, Text }`
      (`good|warn|bad` consts `model.EvidenceGood/Warn/Bad`), json key `evidence`.
      `http_engine` prefers the model's `evidence[]`; otherwise
      `screening.DeriveEvidence(reasons, risk)` — one line per reason toned by risk band
      (`≥0.66` bad · `≥0.33` warn · else good). `reasons []string` + `RawEvidence` kept.
- [x] Risk score scale — stored `0.0–1.0` verbatim everywhere; `model.riskTo100` (single
      conversion point) → integer `0–100` in `ScreeningView.risk_score` and the new
      `EngineView.risk_score`. `ScreeningView.Engine` is now `*EngineView` (never-null
      slices, 0–100 risk).
- [x] `INSUFFICIENT_IMAGE_QUALITY` — kept as a first-class `verdict`. Added
      `Verdict.Band()` (→ genuine/suspicious/fake; INSUFFICIENT + PENDING → SUSPICIOUS)
      and `verdict_band` on `ScreeningView` + `EngineView`. **Frontend:** render
      INSUFFICIENT as its own "retake photo" badge, fall back to `verdict_band` styling.
- [x] Tests — `http_engine_test.go` (derived tone, structured `extracted_fields` +
      model-supplied tone passthrough, flat-map normalisation); `model/screening_view_test.go`
      (`Verdict.Band`, `riskTo100` rounding, `EngineView`, never-null slices);
      `screening_service_test.go` updated to the 0–100 scale.
- [x] Docs — `backend-architecture.md` §6, `database-design.md` §3, `BACKEND_GUIDE.md` §12.

### Phase D — deferred (needs the `passport-model` response shape, coordinate with teammates)
- [ ] The model actually emitting per-field `confidence` and its own `evidence` tone
      (backend already consumes both when present; derivation is the fallback).

## Phase E — Dashboard & reports aggregation  *(all new endpoints)*
- [ ] Define **"accuracy"** — the UI shows team/verifier/system accuracy %. Proposed:
      share of decided screenings where `officer_decision` agrees with the engine
      verdict band (accept↔GENUINE, escalate↔SUSPICIOUS, reject↔FAKE). Needs sign-off.
- [ ] `GET /api/dashboard/summary` — role-aware payload:
  - [ ] verifier — my screenings today, my verdict split, my pending decisions,
        shift counters.
  - [ ] admin — region: verifications today (+trend), team accuracy, avg decision
        time, escalated count, weekly volume `[{day, genuine, suspicious, fake}]`,
        verifier roster `[{name, checkpoint, online, today, accuracy}]`, flagged cases.
  - [ ] superadmin — org: #checkpoints, #admins, #verifiers, screenings today,
        system accuracy, checkpoints table, admins table `[{name, region, team, accuracy}]`.
- [ ] `GET /api/reports` (admin, region) — doc-type breakdown `[{doc, count}]`,
      by-checkpoint breakdown `[{checkpoint, verifiers, today}]`, fake-detection rate,
      weekly volume, avg decision time, escalated cases.
- [ ] Mongo aggregation pipelines + indexes (`region+created_at`, `officer_id+created_at`,
      `verdict+created_at`, `officer_decision.decided_at`).
- [ ] `service/analytics_service.go` — one place for all aggregations; cache the
      heavier org rollups (short TTL) if slow.
- [ ] Tests — seeded screenings → expected counts, region isolation, verdict split.

## Phase F — Face verification (PS Module 4 — UI verifier dashboard card)
- [ ] `internal/face` — `FaceEngine` interface (mirrors `screening.Engine`) +
      `httpFaceEngine` (`POST {FACE_SERVICE_URL}/verify`, X-API-Key) + `MockFaceEngine`.
- [ ] Config — `FACE_ENGINE` (http|mock), `FACE_SERVICE_URL`, `FACE_SERVICE_API_KEY`,
      `FACE_SERVICE_TIMEOUT`, `FACE_MATCH_THRESHOLD` (default `0.85` — UI shows an 85% line).
- [ ] `model/screening.go` — `FaceVerification { Score float64, Matched bool,
      Threshold float64, CapturedImageFileID, VerifiedAt }`, pointer, embedded once.
- [ ] `POST /api/screenings/:id/face` (verifier) — multipart `capture` (JPEG/PNG) →
      GridFS → `FaceEngine.Verify(docPortrait, capture)` → persist `face_verification`.
      Audit `screening.face_verified`.
- [ ] `GET /api/screenings/:id/face-image` — stream the live capture.
- [ ] Surface `face_verification` in `ScreeningView`; feed a low match into `flags`
      (`face_mismatch`) and the risk narrative.
- [ ] Tests — mock engine match/no-match, threshold boundary, image round-trip.

## Phase G — Audit read API + expanded actions  (UI: admin AuditLog, superadmin AuditTrail)
- [x] `AuditRepository.List(ctx, filter, cursor, limit)` *(done 2026-09-07 as `List`,
      not `Find`)* — additive, repo stays Insert-only otherwise (no update/delete).
- [ ] `model/audit.go` — new action constants: `user.role_changed`,
      `user.updated`, `user.disabled`, `org.settings_updated`. *(`auth.login`,
      `user.password_reset`, `user.password_changed`, `checkpoint.created`,
      `checkpoint.updated` already existed pre-Phase G.)*
- [ ] Denormalise `actor_name` onto each `AuditLog` at write time (UI lists the actor's
      display name without N+1 lookups). *(`region` denormalisation done 2026-09-07 —
      see below; `actor_name` still missing.)*
- [x] `GET /api/audit-logs` *(done 2026-09-07)* — verifier (own `user_id`) / admin (own
      region, fail-closed if unset) / superadmin (org), via `middleware.ScopeAuditToActor`.
      Filters implemented: `?action=`, `?region=`. **Not yet implemented:**
      `?category=decision|user|login`, `?actor=`, `?reference_type=`, `?reference_id=`.
      Cursor-paginated, newest first.
- [ ] Wire the new audit writes into user update / role change / disable / settings
      services. *(Checkpoint create/update already write audit entries — region-stamped
      as of 2026-09-07.)*
- [ ] Tests — filter by category, region isolation, append-only invariant. *(No test
      file added with the 2026-09-07 commit — still open.)*

## Phase H — User management (UI: admin Verifiers, superadmin Admins)
- [~] `GET /api/users?role=&region=&status=` — region scoping done 2026-09-07
      (`UserFilter.Region`, admin auto-scoped by `middleware.RegionScope`, fail-closed
      if unset; superadmin can pass `?region=`). **Still missing:** `role=` / `status=`
      query filters.
- [ ] `PATCH /api/users/:id` — update `checkpoint_id`, `region`, `status`
      (enable/disable). Role change is **superadmin only**. Audit `user.updated` /
      `user.role_changed` / `user.disabled`.
- [ ] Verifier/admin detail drawer data — recent screenings (reuse
      `GET /api/screenings?officer_id=`), today count + accuracy (from analytics),
      `member since` (already `created_at` in `UserView`), checkpoints managed
      (for an admin: `GET /api/checkpoints?admin_id=`).
- [ ] `POST /api/users` — already exists; extend to take `region` / `checkpoint_id`
      and enforce the creator's scope (admin can only create verifiers in own region).
- [ ] Tests — scope enforcement on create/list/patch, disable blocks login (existing).

## Phase I — Presence / online status  *(low priority — UI shows online/offline dots)*
- [ ] `User.LastActiveAt` — bumped by a throttled touch in `Authenticate` (once/min) or
      an explicit `POST /api/users/heartbeat` from the SPA.
- [ ] "Online" = `last_active_at` within `PRESENCE_WINDOW` (default 5m). Checkpoint
      "x/y online" derived in the analytics service.
- [ ] Acceptable stopgap for the demo: derive from last successful login.

## Phase J — Settings  *(UI: superadmin Settings — mostly policy, some static)*
- [ ] `settings` collection, single doc — `security_policy { mfa_required,
      password_rotation_days, session_timeout }`. `GET /api/settings` (admin+superadmin
      read), `PUT /api/settings` (superadmin). Audit `org.settings_updated`.
- [ ] Enforce `session_timeout` via `JWT_ACCESS_TTL` (or a claim check);
      `password_rotation_days` checked at login (warn/force).
- [ ] MFA — larger lift; mark out-of-scope for the hackathon unless asked. Toggle
      persists but is not enforced yet.
- [ ] Org-profile block (org / department / PS id / category) is static — a constant,
      no endpoint.
- [ ] Role-permissions matrix in the UI is static documentation — no endpoint.

## Phase K — Contract alignment & frontend wiring
- [ ] `CORS_ALLOW_ORIGINS` — add the Vite dev origin (`http://localhost:5173`).
- [ ] Reference number — backend `SCR-<yyyymmdd>-<00001>` stays; UI mock uses
      `SC-88291`. Frontend adopts the real format (no backend change).
- [ ] `ScreeningView` final review against UI field expectations — `risk_score` 0–100,
      lowercase `doc_type`, `image_url`, `reference_no`, `verdict`, `officer_decision`,
      `face_verification`, `extracted_fields[]`, `evidence[]`.
- [ ] Frontend — replace `src/data/*.js` mock modules with an API client; wire
      `SignIn` → `POST /api/auth/login`; token storage + silent refresh; route guards
      keyed off `user.role`; `HeaderShell` role from the token, not `ROLES` constant.

## Verification (repeat from the base checklist)
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` green on real Mongo
- [ ] `docs/backend/*.md` + `README.md` updated — 3-role table, region model, new
      endpoints, decision enum, error domains (add `7xxxx` checkpoints, `8xxxx` face)
- [ ] `.env.example` — new `FACE_*`, `SUPERADMIN_*`, `PRESENCE_WINDOW` keys
- [ ] Update memory `ps188-backend-stack.md` — 3 roles + regions, decision enum change

---

# Verification architecture — POC scope calls  *(`docs/PS188_VERIFICATION_ARCHITECTURE.md`)*

We are following `docs/PS188_VERIFICATION_ARCHITECTURE.md`. For the SIH POC (demo, not
production-efficient) the items below are **deliberately deferred** — each is a real
improvement the doc describes, none is needed to demo Passport + Aadhaar end-to-end.
Revisit after the happy path runs through the UI.

> "The model" = **`passport-model/`** (FastAPI `server.py` + `Dockerfile` + `render.yaml`,
> deploys to Render / Singapore) — NOT the older `Al-Based-Fake-Identity-Document-Screening-System/`.
> The `predict_pipeline.py` / `indian_passport_verifier.py` / `aadhaar_verifier.py`
> files are byte-identical between the two; `passport-model/` is the one with the HTTP
> server and is what `SCREENING_SERVICE_URL` points at.

## Do first — `httpEngine` ↔ `passport-model/server.py` contract mismatches
*(done 2026-09-03 — real screening now hits `https://passport-model.onrender.com`)*
- [x] `screening/http_engine.go` — path aligned to `POST /api/v1/verify`.
- [x] `screening/http_engine.go` `docTypeParam()` — `national_id` → `aadhaar` (was `aadhar`).
- [x] Response envelope — `predictResponse` carries `success/filename/doc_type` too;
      `extracted_fields` / `evidence` parsed when present (Phase D, done 2026-09-04).
      `{error:{code,message}}` failure envelope is parsed and surfaced in the wrapped error.
- [x] Config defaults — `SCREENING_ENGINE=http`,
      `SCREENING_SERVICE_URL=https://passport-model.onrender.com`,
      `SCREENING_SERVICE_API_KEY=midv2020-secret-api-key-2026`, timeout `60s`.
      `.env.example` + `.env.development.local` updated.
- [x] Tests — `http_engine_test.go` path + error-code passthrough + `aadhaar` mapping.
- [x] Docs — `backend-architecture.md` §6, `BACKEND_GUIDE.md` §12.

## Deferred to post-POC

### Keep the deterministic checks in Python — don't port to Go yet  *(doc §3 / §9)*
- [ ] Doc moves ICAO 9303 + Verhoeff + format regex into the Go backend. `passport-model`
      already runs all of it and emits `verdict` + `evidence_table`; the Go backend
      stores `evidence_table` verbatim today (`engine.evidence` is `bson.M`). Port to Go
      later, using the Python as a test oracle (run both, assert equal) — checksum bugs
      are silent.
- [ ] Doc §2 "strip the model service to CNN + ELA only" — deferred with the above;
      `passport-model` keeps running the full pipeline for now.

### Evidence aggregator / weighted risk score in Go  *(doc §4 step 6, §9 phase 6)*
- [ ] Doc wants the backend to combine validation + tampering + face + blacklist into a
      weighted `risk_score` and verdict band. Biggest new build. For the POC,
      `passport-model` keeps producing the verdict/band and Go passes it through.
      Blacklist stays **additive** — a `blacklist_hit` flag, not folded into the score.

### Split OCR / tampering / face into separate deployed services  *(doc §2 / §3)*
- [ ] Doc shows three independent services. For the POC keep them as modules/routes in
      the one `passport-model` FastAPI process (ideally one `POST /verify` returning
      ocr + cnn + ela + face). The architectural boundary is the response contract, not
      process isolation — split later only if it matters.
- [ ] Backend parallel fan-out (doc §4 step 2) — deferred with the split. A single
      sequential chain (model → validation → blacklist → aggregate) is enough for one
      verifier screening one document and far easier to debug. (The doc's "3 parallel
      calls" also has a hidden dependency: the face service needs the doc face crop /
      `face_bbox` from OCR output.)

### `verification_records` collection  *(doc §6)*
- [ ] Don't add it. `screenings` (full case) + `audit_logs` (append-only) already hold
      everything it lists (`checkpoint_id`, `verifier_id`, `verdict`, `risk_score`,
      `timestamp`). A third collection is drift.

### `doc_number_hash` at rest  *(doc §6)*
- [ ] Nice privacy hardening. `submitted_number` is stored plaintext today and that's
      fine for a POC. Post-SIH.

### Face-match liveness / anti-spoofing  *(doc §5 `live_capture_liveness_ok`)*
- [ ] Real liveness is a research problem. Phase F does plain embedding cosine
      similarity (doc face crop vs webcam capture), `match = score ≥ threshold`, no
      liveness. Mark liveness explicitly out of scope in the architecture doc.

### Visa / Driving License / Permit  *(doc §7 / §8)*
- [ ] Deferred until Passport + Aadhaar run end-to-end. Visa reuses the passport MRZ
      7-3-1 checksum math; DL is format-regex + blacklist only (no check digit); Permit
      needs a sample document before any work. Backend already carries all five
      `DocType` values, so this is model-side + validation work, not schema work.
