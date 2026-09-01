# PS188 Backend — Architecture & Module Map

> Companion to **`BACKEND_GUIDE.md`** (the style bible — error convention,
> repositories, services, handlers, testing, Docker). That guide defines **how**
> code is written; this document defines **what** we build for PS188: the module
> inventory, every endpoint, the screening lifecycle, error-code domains, the
> external model boundary, and the build order.
> Schema source of truth: **`database-design.md`** (MongoDB, single database, RBAC).

---

## 1. What PS188 Is

**AI-Based Fake Identity & Document Screening System** (SIH 2026, PS 26188 —
Ministry of Home Affairs / SSB). A border-checkpoint officer submits an identity or
travel document (passport, visa, national ID, driving licence, permit); the platform
runs it through an AI screening pipeline and returns a **risk score + explainable
verdict** so the officer can decide faster and more accurately, with a full digital
trail for later investigation.

The four PS modules:

| PS Module | Where it runs | This backend's role |
|---|---|---|
| 1 — OCR Extraction | External Python/FastAPI model | Sends the image; stores `extracted_fields` |
| 2 — Document Validation | External model (checksums, MRZ, format rules) | Stores the rule outcomes in `evidence` |
| 3 — Tampering Detection (core AI) | External model (CNN, ELA, stamp/photo/text) | Stores the forensic evidence + drives `risk_score` |
| 4 — Face Verification | *Future module* (see §7) | Not in the current slice |

**The AI itself is not in this repo.** It is
`Al-Based-Fake-Identity-Document-Screening-System/predict_pipeline.py`, wrapped in a
**separately hosted FastAPI service**. This backend is the system of record and the
officer-facing API; it calls that model over HTTP (§6).

---

## 2. Stack & Deployment Model

Go 1.26 · Gin · MongoDB (`mongo-driver/v2`) · JWT · bcrypt(12) · `log/slog` ·
Docker Compose (backend + MongoDB).

**Single deployment, role-based access.** One MongoDB database. No multi-tenancy, no
org registry, no per-agency databases — this is one checkpoint / one agency install.
Two roles:

| Role | Can |
|---|---|
| `supervisor` | the checkpoint officer — submit screenings, view them, record a decision |
| `admin` | manage user accounts (create / list); also has full read access to screenings |

Accounts are created by an `admin`. First run seeds one bootstrap admin from
`ADMIN_USERNAME` / `ADMIN_PASSWORD` (only while the `users` collection is empty).

> "Officer" throughout this doc and the code (`officer_id`, `officer_decision`) means
> the human working the checkpoint — always a `supervisor`-role user. It is a job
> description, not a separate RBAC role.

---

## 3. Folder Structure

See `BACKEND_GUIDE.md` §1 for the full tree. In brief:

```
cmd/api/                bootstrap
internal/config         env → Config
internal/database       Mongo connect + EnsureIndexes
internal/apperr         AppError + ERRORS catalog
internal/response       JSON envelopes + Page[T]
internal/model          User, Screening, AuditLog (+ Views, enums)
internal/repository     UserRepository, ScreeningRepository, AuditRepository
internal/service        AuthService, UserService, ScreeningService
internal/screening      Engine iface + httpEngine + MockEngine   ← external model client
internal/storage        FileStore iface + GridFS impl            ← document images
internal/platform/jwt   token Manager
internal/transport/http router + auth/user/screening handlers
internal/testsupport    RequireMongo(t)
pkg/logger              slog factory
```

---

## 4. Module Inventory & API Endpoints

All routes mount under `/api`. Middleware order (guide §10):
`Recovery → ErrorHandler → RequestLog → CORS → RateLimit`, then per-group
`Authenticate → RequireRole → handler`. List endpoints use cursor pagination
(`?cursor=&limit=`, guide §6). Success/error envelopes per guide §5.

### 4.0 Health — `/health`

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/health` | public | `{status, mongo}` — pings MongoDB; `503` if down |

### 4.1 Auth — `/api/auth`

| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `/login` | public | `{username, password}` → `{user, access_token, refresh_token}`. Unknown user and wrong password return the same `INVALID_CREDENTIALS` (20007). |
| POST | `/refresh-token` | public | `{refresh_token}` → `{access_token}`. Re-checks the account still exists and is `active`. |

Access token TTL `JWT_ACCESS_TTL` (default 1h); refresh `JWT_REFRESH_TTL` (default 30d).
`TokenData` = `{UserID, Username, Role}`. Stateless — logout is client-side token drop.

### 4.2 Users — `/api/users`  (all require auth)

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/profile` | any authed | Own `UserView` |
| POST | `/` | admin | Create a user: `{username, full_name, email, password, role}`. `USERNAME_TAKEN` / `EMAIL_TAKEN` on conflict, `INVALID_ROLE` for an unknown role. Audit: `user.created`. |
| GET | `/` | admin | Cursor-paginated `UserView[]` |

### 4.3 Screenings — `/api/screenings`  (all require auth)

The core module. A screening case = one document submitted at a checkpoint + the
engine's result + (optionally) an officer's decision.

| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `/` | supervisor | **Submit.** `multipart/form-data`: `document` (JPEG/PNG, ≤ `MAX_UPLOAD_BYTES`) + `doc_type` + optional `doc_number`, `mrz_line1`, `mrz_line2`, `checkpoint_id`. Flow in §5. Returns the `ScreeningView` (status `completed` or `failed`). Audit: `screening.submitted`. |
| GET | `/` | any authed | List. Filters `?verdict=&doc_type=&status=&checkpoint_id=`, cursor-paginated, newest first. |
| GET | `/:id` | any authed | Full detail incl. `engine.evidence` (the explainability table) and `engine.reasons`. |
| GET | `/:id/image` | any authed | Streams the stored document image (from GridFS), `Content-Type` sniffed. |
| POST | `/:id/decision` | supervisor | Record the officer's manual call: `{decision: clear\|refer\|detain, reason}`. Exactly one per screening — a second call → `ALREADY_DECIDED` (40002). Not allowed while `status=processing` → `SCREENING_NOT_COMPLETED` (40004). Audit: `screening.decided`. |

### 4.4 Audit trail (internal)

`audit_logs` is written by services on every state-changing operation
(`user.created`, `screening.submitted`, `screening.decided`) with `old_data`/`new_data`
snapshots and the actor's IP. **Append-only** — `AuditRepository` exposes `Insert`
only. A read API (`GET /api/audit-logs`, admin) is a planned addition (§7), not in the
current slice.

---

## 5. The Screening Lifecycle

```
        POST /api/screenings   (supervisor, multipart image)
                    │
                    ▼
     ┌─ validate doc_type, file type & size ─┐  → 4xx / 5xx, nothing stored
                    │
                    ▼
        store image in GridFS  →  image_file_id
                    │
        allocate reference_no  =  SCR-<yyyymmdd>-<00001>   (gapless per-day counter)
                    │
                    ▼
        insert screening   { status: processing, verdict: PENDING }
                    │
                    ▼
        call Engine.Screen(image, doc_type, doc_number, mrz…)   ── the external model
                    │
        ┌───────────┴─────────────┐
        ▼                         ▼
   engine error              engine result
        │                         │
   SetResult(                SetResult(
     status: failed,           status: completed,
     verdict: PENDING,         verdict: <GENUINE|SUSPICIOUS|FAKE|INSUFFICIENT_IMAGE_QUALITY>,
     failure_reason: …)        risk_score, engine.evidence, engine.reasons, extracted_fields)
        │                         │
        └───────────┬─────────────┘
                    ▼
        audit: screening.submitted        ← case is now returned to the officer
                    │
                    ▼
        POST /api/screenings/:id/decision   (supervisor)
             guard: status != processing ; officer_decision not already set
                    ▼
        embed officer_decision { decision: clear|refer|detain, reason, decided_by, decided_at }
        audit: screening.decided
```

**Rules**

1. A failed engine call is **not** a failed request. The case is persisted with
   `status=failed` + `failure_reason` and returned `201` — the officer still sees the
   document and can `detain`/`refer` on judgement. This is deliberate: the model being
   down must not block a checkpoint.
2. `reference_no` is allocated from an atomic per-day counter
   (`counters` collection, `FindOneAndUpdate` + `$inc` + upsert) — gapless, race-safe.
3. Exactly one `officer_decision` per screening, enforced by a **conditional update**
   (`officer_decision: {$exists: false}` in the filter). A losing concurrent writer
   gets `mongo.ErrNoDocuments`, mapped to `ALREADY_DECIDED`.
4. `verdict` on the document mirrors `engine.verdict` verbatim (`UPPER_SNAKE`). While
   `processing`/`failed` it is `PENDING`.
5. The engine's `evidence_table` is stored **verbatim** as `engine.evidence` — full
   explainability for disputes, never reshaped.

---

## 6. External Screening Engine

`internal/screening/` — see `BACKEND_GUIDE.md` §12.

**Contract** (matches `predict_pipeline.py`'s report):

```
POST {SCREENING_SERVICE_URL}/predict            multipart/form-data
  image=<file>
  doc_type=passport | aadhar | auto            (national_id → aadhar; others → auto)
  doc_number=<string>        (optional)
  mrz_line1=<string>         (optional, passports)
  mrz_line2=<string>         (optional, passports)
  header  X-API-Key: {SCREENING_SERVICE_API_KEY}

200 → {
  "verdict": "GENUINE" | "SUSPICIOUS" | "FAKE" | "INSUFFICIENT_IMAGE_QUALITY",
  "risk_score": 0.0-1.0,
  "reasons": ["...", "..."],
  "extracted_fields": { "passport_number": "...", ... },   // optional
  "evidence_table": { "cnn_score": 0.6, "ela_forensics": {...}, "mrz_checksums": {...}, ... }
}
```

- Transport error / timeout / non-200 → `SCREENING_ENGINE_UNAVAILABLE` (60001·502).
- Unparseable body / missing verdict → `SCREENING_ENGINE_BAD_RESPONSE` (60002·502).
- `SCREENING_ENGINE=mock` swaps in `MockEngine` (deterministic, offline) — the default
  in `docker-compose.yml` so the stack runs with no model attached.
- Config: `SCREENING_SERVICE_URL`, `SCREENING_SERVICE_API_KEY`,
  `SCREENING_SERVICE_TIMEOUT` (default 30s).

The pipeline currently branches on `passport` / `aadhar`. As the FastAPI wrapper grows
to cover visa / driving-licence / permit, only `docTypeParam()` in `http_engine.go`
changes — the rest of the backend already carries all five `DocType`s.

---

## 7. Error Catalog — PS188 domains

`1xxxx` common and `2xxxx` auth are generic. Domain ranges:

| Range | Domain | Examples |
|---|---|---|
| 3xxxx | Users | `USER_NOT_FOUND` 30001·404, `USERNAME_TAKEN` 30002·409, `EMAIL_TAKEN` 30003·409, `INVALID_ROLE` 30004·422 |
| 4xxxx | Screenings | `SCREENING_NOT_FOUND` 40001·404, `ALREADY_DECIDED` 40002·409, `INVALID_DOC_TYPE` 40003·422, `SCREENING_NOT_COMPLETED` 40004·409, `INVALID_DECISION` 40005·422 |
| 5xxxx | Files / storage | `FILE_REQUIRED` 50001·400, `FILE_TOO_LARGE` 50002·413, `INVALID_FILE_TYPE` 50003·422, `STORAGE_FAILED` 50004·500 |
| 6xxxx | External screening engine | `SCREENING_ENGINE_UNAVAILABLE` 60001·502, `SCREENING_ENGINE_BAD_RESPONSE` 60002·502 |

All in `internal/apperr/apperr.go`'s `ERRORS` — never an inline `&AppError{}` (guide §4).

---

## 8. Future Modules (documented, not yet built)

These extend the same model → repository → service → handler pattern; each is one
vertical slice per the guide's checklist.

| Module | Sketch |
|---|---|
| **Checkpoints** (`/api/checkpoints`) | Named checkpoint registry so `checkpoint_id` on a screening is a real reference, not a free string. Admin-managed. |
| **Face Verification** (`/api/screenings/:id/face`, PS Module 4) | Officer captures a live photo; a `FaceEngine` (interface, like `screening.Engine`) compares it to the document portrait; result appended to the screening as `face_verification { score, matched, captured_image_file_id }`. |
| **Watchlist** (`/api/watchlist`) | Blacklisted / expired document numbers and identities; checked during `Submit`, contributing to `risk_score` and raising a flag. |
| **Cases** (`/api/cases`) | Group multiple screenings of the same traveller (multiple-identity detection); investigator notes; export. |
| **Audit read API** (`GET /api/audit-logs`, admin) | Paginated, filters `?action=&reference_type=&reference_id=`. Repo stays `Insert`-only; a `Find` method is additive. |
| **Dashboard** (`/api/dashboard/summary`) | Aggregate counts (screenings today, by verdict, pending decisions, engine failures 24h) — single aggregation queries. |
| **Async screening** | If the model gets slow, `Submit` returns `202` with `status=processing` immediately and a worker calls the engine; the lifecycle diagram already accommodates this. |

---

## 9. Testing Strategy (per guide §15)

| Layer | Type | Stubs | Priority scenarios |
|---|---|---|---|
| Repositories | real MongoDB (`RequireMongo`) | none | create+find, not-found mapping, cursor pagination across pages, `SetDecision` conditional-write race, `NextSequence` monotonicity |
| Services | real repos + real GridFS | `screening.MockEngine` only | `Submit` → completed (verdict/risk/reference persisted); `Submit` with engine down → `status=failed` persisted, request still succeeds; `INVALID_DOC_TYPE`; `Decide` once → then `ALREADY_DECIDED`; `INVALID_DECISION`; auth login success / wrong-password / unknown-user indistinguishable / refresh |
| Engine client | `httptest.Server` | the server itself | happy path (verdict + evidence passthrough), non-200 → `UNAVAILABLE`, bad JSON → `BAD_RESPONSE`, API-key header sent |

No test mocks a repository or a service. MongoDB comes from `docker compose up`.

---

## 10. Build Order

1. **Skeleton** — config, apperr, response, logger, jwt, database + `EnsureIndexes`,
   middleware, `main.go`, Docker. *(done)*
2. **Auth + Users** — login, refresh, RBAC, admin-seeded accounts. *(done)*
3. **Screenings vertical slice** — model → repo → service → handlers: submit (GridFS +
   engine call + persist), list, get, image, decision; audit logging. *(done — the
   current slice)*
4. **Checkpoints** registry.
5. **Face Verification** module (PS Module 4) behind a `FaceEngine` interface.
6. **Watchlist** + risk contribution.
7. **Cases** (multiple-identity linking) + **Audit read API** + **Dashboard**.
8. Point `SCREENING_ENGINE=http` at the deployed FastAPI model; load-test; deploy.

Each step = model → repository (+ real-DB tests) → service (+ tests, engine stubbed) →
handler → route registration, per `BACKEND_GUIDE.md` §18.
