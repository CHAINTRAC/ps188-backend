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

**The AI itself is not in this repo.** It is `passport-model/` (FastAPI `server.py`
around `predict_pipeline.py`), **separately hosted** at
`https://passport-model.onrender.com`. This backend is the system of record and the
officer-facing API; it calls that model over HTTP (§6).

---

## 2. Stack & Deployment Model

Go 1.26 · Gin · MongoDB (`mongo-driver/v2`) · JWT · bcrypt(12) · `log/slog` ·
Docker Compose (backend + MongoDB).

**Single deployment, role-based access.** One MongoDB database. No multi-tenancy, no
per-agency databases — this is one agency install. Three roles (names match the
operator-facing UI):

| Role | Scope | Can |
|---|---|---|
| `verifier` | one checkpoint (region inherited) | the checkpoint officer — submit screenings, view them, record a decision, run blacklist checks |
| `admin` | one region | everything a verifier's account needs: create / list verifier accounts, manage the blacklist; full read access to screenings; reset a verifier's password |
| `superadmin` | org-wide | everything an `admin` can, plus manage `admin` accounts and the checkpoint registry |

Admin-level routes are authorised for **both** `admin` and `superadmin`
(`RequireRole(admin, superadmin)`); checkpoint create/patch are `superadmin` only.
Accounts are created by an `admin` or `superadmin`. First run seeds one bootstrap
`superadmin` from `SUPERADMIN_USERNAME` / `SUPERADMIN_PASSWORD` / `SUPERADMIN_EMAIL`
(legacy `ADMIN_*` still read as a fallback; only while the `users` collection is empty).

**Region model.** A verifier is bound to one `checkpoint_id`; its `region` is
resolved from the `checkpoints` registry at create time and denormalised onto the
user. An admin is bound to a `region` (required, must already exist in the
registry). A superadmin has both empty = org-wide. `TokenData` carries `region` +
`checkpoint_id` so request scoping needs no per-call user lookup.

> "Officer" throughout this doc and the code (`officer_id`, `officer_decision`) means
> the human working the checkpoint — always a `verifier`-role user. It is a job
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
internal/model          User, Checkpoint, Screening, Blacklist, AuditLog (+ Views, enums)
internal/repository     User / Checkpoint / Screening / Blacklist / Audit repositories
internal/service        Auth / User / Checkpoint / Screening / Blacklist services
internal/screening      Engine iface + httpEngine + MockEngine   ← external model client
internal/storage        FileStore iface + local-disk / GridFS impls ← document images
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
| POST | `/login` | public | `{identifier\|email\|username, password}` → `{user, access_token, refresh_token}`. `identifier` matches username **or** email (the UI submits email). Unknown user and wrong password return the same `INVALID_CREDENTIALS` (20007). Audit on success: `auth.login` (actor, IP, region). |
| POST | `/refresh-token` | public | `{refresh_token}` → `{access_token}`. Re-checks the account still exists and is `active`; re-issues `region` / `checkpoint_id` claims from the current record. |

Access token TTL `JWT_ACCESS_TTL` (default 1h); refresh `JWT_REFRESH_TTL` (default 30d).
`TokenData` = `{UserID, Username, Role, Region, CheckpointID}`. Stateless — logout is client-side token drop.

### 4.2 Users — `/api/users`  (all require auth)

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/profile` | any authed | Own `UserView` (incl. `region`, `checkpoint_id`) |
| POST | `/` | admin / superadmin | Create a user: `{username, full_name, email, password, role, region?, checkpoint_id?}`. `checkpoint_id` is **required for a verifier** (region resolved from it); `region` is **required for an admin** (must exist in the registry). `USERNAME_TAKEN` / `EMAIL_TAKEN` on conflict, `INVALID_ROLE` for an unknown role, `MISSING_SCOPE_FIELD` (30006), `UNKNOWN_REGION` (80004). Audit: `user.created`. |
| GET | `/` | admin / superadmin | Cursor-paginated `UserView[]`. Region auto-scoped (admin = own region, fail-closed if unset; superadmin org-wide or `?region=`). Extra filters `?role=&status=`. |
| PATCH | `/:id` | admin / superadmin | Update `status` (enable/disable), scope (`checkpoint_id` for a verifier, `region` for an admin), or `role`. **Role change is superadmin-only**; changing a role re-derives the scope for the new role (checkpoint required for verifier, region for admin, both cleared for superadmin). An admin may edit **only a verifier in their own region**. Nobody may disable their own account or change their own role → `CANNOT_MODIFY_SELF` (30007). `FORBIDDEN` (20006), `MISSING_SCOPE_FIELD` (30006), `UNKNOWN_REGION` (80004). Audit: `user.role_changed` / `user.disabled` / `user.updated` (most-specific wins). |
| POST | `/change-password` | any authed | `{current_password, new_password}` — verifies current, rehashes. `INVALID_CURRENT_PASSWORD` (30005). Audit: `user.password_changed`. |
| POST | `/:id/reset-password` | admin / superadmin | Admin resets a `verifier` in their own region; superadmin resets anyone. Returns `{temp_password}`. `FORBIDDEN` (20006) out of scope. Audit: `user.password_reset`. |

### 4.3 Screenings — `/api/screenings`  (all require auth)

The core module. A screening case = one document submitted at a checkpoint + the
engine's result + (optionally) an officer's decision.

| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `/` | verifier | **Submit.** `multipart/form-data`: `document` (JPEG/PNG, ≤ `MAX_UPLOAD_BYTES`) + `doc_type` + optional `doc_number`, `mrz_line1`, `mrz_line2`, `holder_name`, `dob`, `nationality`, `expiry_date`. `checkpoint_id` and `region` come from the verifier's token (stamped at account creation), not the form. Flow in §5 — includes the post-engine blacklist + expiry checks that populate `flags[]` / `blacklist_matches[]`. Returns the `ScreeningView` (status `completed` or `failed`). Audit: `screening.submitted`. |
| GET | `/` | any authed | List. **Auto-scoped** by `middleware.ScopeToActor`: verifier → own `officer_id` (UI "My History"), admin → own `region`, super admin → unscoped. Extra filters `?verdict=&doc_type=&status=&checkpoint_id=&decided=false&decision=`. Cursor-paginated, newest first. "Flagged for review" (admin dashboard) = `?verdict=SUSPICIOUS` (or `FAKE`) `&decided=false` — no dedicated route. |
| GET | `/:id` | any authed | Full detail incl. `engine.evidence` (the explainability table) and `engine.reasons`. |
| GET | `/:id/image` | any authed | Streams the stored document image (via `FileStore` — local disk by default, GridFS if configured), `Content-Type` sniffed. |
| POST | `/:id/decision` | verifier | Record the officer's manual call: `{decision: accept\|escalate\|reject, reason}` (names match the UI). Exactly one per screening — a second call → `ALREADY_DECIDED` (40002). Not allowed while `status=processing` → `SCREENING_NOT_COMPLETED` (40004). Audit: `screening.decided` (`new_data.detail` = `screening.decided · ACCEPT\|ESCALATE\|REJECT`). |

### 4.4 Blacklist — `/api/blacklist`  (all require auth)

Blacklisted document numbers and identities — the PS lines "expired or blacklisted
travel documents" and "multiple identities used by the same person". Admin-managed;
the read-side `check` is open to verifiers so the checkpoint can screen against it.
Entries are deactivated, never deleted.

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/check` | any authed | `?doc_number=&name=&dob=&nationality=` → `{hit, matches[]}`. Normalised (case-insensitive) match against **active** entries. Never blocks — informational. |
| POST | `/` | admin / superadmin | Add an entry: `{kind: document\|identity, doc_number?, name?, dob?, nationality?, reason, source?}`. `INVALID_BLACKLIST_KIND` (70003), `BLACKLIST_FIELDS_MISSING` (70004) if the kind's required fields are absent, `BLACKLIST_ENTRY_EXISTS` (70002) if an active entry already matches. Audit: `blacklist.added`. |
| GET | `/` | admin / superadmin | List. Filters `?kind=&active=`, cursor-paginated, newest first. |
| GET | `/:id` | admin / superadmin | One `BlacklistView`. `BLACKLIST_ENTRY_NOT_FOUND` (70001). |
| POST | `/:id/deactivate` | admin / superadmin | Flip `active` to `false`. Audit: `blacklist.deactivated`. |

> `BlacklistService.Check` is wired into `ScreeningService.Submit` (§5): after the
> engine call it probes the submitted / extracted document number and identity. A
> hit raises the `blacklist_hit` flag, records `blacklist_matches[]`, appends an
> `engine.reasons` note, and bumps `risk_score` by `0.25`. It never blocks — the
> officer still decides.

### 4.6 Checkpoints — `/api/checkpoints`  (all require auth)

The checkpoint registry. `code` (e.g. `CP-04`, unique, upper-cased) is the external
id stamped on users and screenings; `region` is authoritative here.

| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `/` | superadmin | `{code, region, admin_id?}` → `CheckpointView` (status defaults `active`). `CHECKPOINT_EXISTS` (80002). Audit: `checkpoint.created`. |
| GET | `/` | admin / superadmin | List. Admin sees **own region only**; superadmin sees all (optional `?region=&admin_id=`). Cursor-paginated. |
| GET | `/:code` | admin / superadmin | One `CheckpointView`. `CHECKPOINT_NOT_FOUND` (80001). |
| PATCH | `/:code` | superadmin | `{admin_id?, status?}` — `code` / `region` immutable. `INVALID_CHECKPOINT_STATUS` (80003). Audit: `checkpoint.updated`. |

`CheckpointService.Resolve(code) → region` and `RegionExists(region)` back the
user-create validation above.

### 4.7 Analytics — `/api/dashboard/summary` + `/api/reports`  (all require auth)

`service/analytics_service.go` is the single home for every dashboard / report
aggregation. It reads through `ScreeningRepository.Aggregate(pipeline)` (one
`$facet` per endpoint — one round trip) plus plain counts for the org totals, and
never writes. All day boundaries are **UTC** (deferred: timezone-aware).

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/dashboard/summary` | any authed | **Role-aware** — the service reads the principal's role/region and scopes every number. Common fields: `screenings_today` / `screenings_total`, `decided_today` / `decided_total`, `pending_decisions`, `escalated`, `avg_decision_seconds`, `verdict_split {genuine,suspicious,fake}`, `weekly_volume [{date,day,genuine,suspicious,fake}]` (last 7 UTC days). Verifier → own screenings only. Admin → region, plus `verifier_activity` / `checkpoint_activity` (`[{id,today,total}]`) and `flagged_cases` (undecided + non-genuine / blacklist-hit, ≤8). Superadmin → org, plus `totals {checkpoints,admins,verifiers}`, `checkpoint_activity`, `flagged_cases`. |
| GET | `/reports` | admin / superadmin | Admin = own region (fail-closed if unset); superadmin = org-wide or `?region=`. `total_screenings`, `fake_rate` (percent, 1 dp), `escalated`, `avg_decision_seconds`, `weekly_volume`, `doc_type_breakdown [{doc_type,count}]`, `checkpoint_breakdown [{id,today,total}]`. Verifier headcount per checkpoint is org-structure data — the SPA joins it from `GET /users`. |

Accuracy % (team / verifier / system) is **deliberately not implemented** — its
definition is unsigned-off (see `todo.md` Phase E).

### 4.5 Audit trail (internal)

`audit_logs` is written by services on every state-changing operation
(`auth.login`, `user.created`, `user.updated`, `user.role_changed`,
`user.disabled`, `user.password_changed`, `user.password_reset`,
`screening.submitted`, `screening.decided`, `blacklist.added`,
`blacklist.deactivated`, `checkpoint.created`, `checkpoint.updated`) with
`old_data`/`new_data` snapshots and the actor's IP. **Append-only** —
`AuditRepository` exposes `Insert` and `List` (no update/delete). The read API
`GET /api/audit-logs` is scoped per role by `middleware.ScopeAuditToActor`
(verifier = own `user_id`, admin = own region fail-closed, superadmin = org-wide),
cursor-paginated newest first, filters `?action=&region=`.

---

## 5. The Screening Lifecycle

```
        POST /api/screenings   (verifier, multipart image)
                    │
                    ▼
     ┌─ validate doc_type, file type & size ─┐  → 4xx / 5xx, nothing stored
                    │
                    ▼
        store image via FileStore (local disk by default, or GridFS)  →  image_file_id
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
        post-engine checks:  BlacklistService.Check(doc_number + identity)
                             + expiry check on submitted/extracted expiry date
             on a hit → SetChecks( flags[], blacklist_matches[],
                        risk_score += 0.25 (blacklist) / 0.15 (expired),
                        append engine.reasons )   ── verdict unchanged, never blocks
                    │
                    ▼
        audit: screening.submitted        ← case is now returned to the officer
                    │
                    ▼
        POST /api/screenings/:id/decision   (verifier)
             guard: status != processing ; officer_decision not already set
                    ▼
        embed officer_decision { decision: accept|escalate|reject, reason, decided_by, decided_at }
        audit: screening.decided
```

**Rules**

1. A failed engine call is **not** a failed request. The case is persisted with
   `status=failed` + `failure_reason` and returned `201` — the officer still sees the
   document and can `reject`/`escalate` on judgement. This is deliberate: the model
   being down must not block a checkpoint.
2. `reference_no` is allocated from an atomic per-day counter
   (`counters` collection, `FindOneAndUpdate` + `$inc` + upsert) — gapless, race-safe.
3. Exactly one `officer_decision` per screening, enforced by a **conditional update**
   (`officer_decision: {$exists: false}` in the filter). A losing concurrent writer
   gets `mongo.ErrNoDocuments`, mapped to `ALREADY_DECIDED`.
4. `verdict` on the document mirrors `engine.verdict` verbatim (`UPPER_SNAKE`). While
   `processing`/`failed` it is `PENDING`.
5. The engine's `evidence_table` is stored **verbatim** as `engine.evidence` — full
   explainability for disputes, never reshaped.
6. Post-engine checks are **advisory**. A blacklist hit or a past expiry date raises
   a `flags[]` entry and nudges `risk_score` (clamped to `1.0`) but never changes the
   `verdict` and never blocks the officer's decision. A blacklist lookup failure is
   logged and swallowed — the screening still returns.

---

## 6. External Screening Engine

`internal/screening/` — see `BACKEND_GUIDE.md` §12.

**Contract** — `passport-model/server.py`, hosted at `https://passport-model.onrender.com`
(Swagger: `/docs`). This is the default `SCREENING_SERVICE_URL`.

```
POST {SCREENING_SERVICE_URL}/api/v1/verify       multipart/form-data
  image=<file>
  doc_type=passport | aadhaar | auto           (national_id → aadhaar; others → auto)
  doc_number=<string>        (optional)
  mrz_line1=<string>         (optional, passports)
  mrz_line2=<string>         (optional, passports)
  header  X-API-Key: {SCREENING_SERVICE_API_KEY}

200 → {
  "success": true,
  "filename": "download.jpg",
  "doc_type": "passport",
  "verdict": "GENUINE" | "SUSPICIOUS" | "FAKE" | "INSUFFICIENT_IMAGE_QUALITY",
  "risk_score": 0.0-1.0,
  "reasons": ["...", "..."],
  "extracted_fields": [ {"label":"document_number","value":"Z1234567","confidence":0.97}, ... ],  (optional)
  "evidence": [ {"tone":"good|warn|bad","text":"..."}, ... ],                                     (optional)
  "evidence_table": { "cnn_score": 0.6, "ela_forensics": {...}, "quality_assessment": {...}, ... }
}

4xx/5xx → { "success": false, "error": { "code": "MODEL_UNAVAILABLE", "message": "..." } }
```

- **`extracted_fields`** (Phase D) — `[]ExtractedField{Label, Value, Confidence}`.
  `parseExtractedFields` also accepts a plain `{label: value}` object (sorted by
  label). Absent → `nil`; the raw `evidence_table` is always kept verbatim as
  `EngineResult.RawEvidence` (`json:"raw_evidence"`).
- **`evidence`** (Phase D) — `[]EvidenceItem{Tone, Text}` for the UI panel. If the
  model omits it, `DeriveEvidence` builds one line per `reason` toned by the risk
  band (`≥0.66` bad · `≥0.33` warn · else good). `reasons []string` is kept as-is.
- **Risk-score scale** — the model's `0.0–1.0` is stored verbatim (`Screening.risk_score`,
  `engine.risk_score`). **Every API projection converts to an integer `0–100`** via
  the single `model.riskTo100` — `ScreeningView.risk_score` and `EngineView.risk_score`
  are `0–100`. `RiskGauge` / history in the UI consume the `0–100` form directly.
- **`INSUFFICIENT_IMAGE_QUALITY`** stays a first-class `verdict`. `ScreeningView`
  also exposes `verdict_band` (`Verdict.Band()` — collapses INSUFFICIENT and PENDING
  to `SUSPICIOUS`) for UI controls that only render genuine/suspicious/fake. Frontend
  SHOULD give INSUFFICIENT its own "retake photo" badge and fall back to the band
  otherwise. *(flagged for frontend)*
- Transport error / timeout / non-200 → `SCREENING_ENGINE_UNAVAILABLE` (60001·502); the
  model's `error.code` is surfaced in the wrapped error.
- Unparseable body / missing verdict → `SCREENING_ENGINE_BAD_RESPONSE` (60002·502).
- `SCREENING_ENGINE=mock` swaps in `MockEngine` (deterministic, offline) — the default
  in `docker-compose.yml` so the stack runs with no model attached.
- Config: `SCREENING_SERVICE_URL` (default `https://passport-model.onrender.com`),
  `SCREENING_SERVICE_API_KEY`, `SCREENING_SERVICE_TIMEOUT` (default 60s).

`server.py` accepts `auto | passport | aadhaar`. As it grows to cover
visa / driving-licence / permit, only `docTypeParam()` in `http_engine.go` changes —
the rest of the backend already carries all five `DocType`s.

---

## 7. Error Catalog — PS188 domains

`1xxxx` common and `2xxxx` auth are generic. Domain ranges:

| Range | Domain | Examples |
|---|---|---|
| 3xxxx | Users | `USER_NOT_FOUND` 30001·404, `USERNAME_TAKEN` 30002·409, `EMAIL_TAKEN` 30003·409, `INVALID_ROLE` 30004·422, `INVALID_CURRENT_PASSWORD` 30005·401, `MISSING_SCOPE_FIELD` 30006·422, `CANNOT_MODIFY_SELF` 30007·403 |
| 4xxxx | Screenings | `SCREENING_NOT_FOUND` 40001·404, `ALREADY_DECIDED` 40002·409, `INVALID_DOC_TYPE` 40003·422, `SCREENING_NOT_COMPLETED` 40004·409, `INVALID_DECISION` 40005·422 |
| 5xxxx | Files / storage | `FILE_REQUIRED` 50001·400, `FILE_TOO_LARGE` 50002·413, `INVALID_FILE_TYPE` 50003·422, `STORAGE_FAILED` 50004·500 |
| 6xxxx | External screening engine | `SCREENING_ENGINE_UNAVAILABLE` 60001·502, `SCREENING_ENGINE_BAD_RESPONSE` 60002·502 |
| 7xxxx | Blacklist | `BLACKLIST_ENTRY_NOT_FOUND` 70001·404, `BLACKLIST_ENTRY_EXISTS` 70002·409, `INVALID_BLACKLIST_KIND` 70003·422, `BLACKLIST_FIELDS_MISSING` 70004·422 |
| 8xxxx | Checkpoints | `CHECKPOINT_NOT_FOUND` 80001·404, `CHECKPOINT_EXISTS` 80002·409, `INVALID_CHECKPOINT_STATUS` 80003·422, `UNKNOWN_REGION` 80004·422 |

All in `internal/apperr/apperr.go`'s `ERRORS` — never an inline `&AppError{}` (guide §4).

---

## 8. Future Modules (documented, not yet built)

These extend the same model → repository → service → handler pattern; each is one
vertical slice per the guide's checklist.

| Module | Sketch |
|---|---|
| **Face Verification** (`/api/screenings/:id/face`, PS Module 4) | Officer captures a live photo; a `FaceEngine` (interface, like `screening.Engine`) compares it to the document portrait; result appended to the screening as `face_verification { score, matched, captured_image_file_id }`. |
| **Cases** (`/api/cases`) | Group multiple screenings of the same traveller (multiple-identity detection); investigator notes; export. |
| **Reports — extended** (`/api/reports`) | Core breakdowns shipped (§4.7). Deferred: fake-detection trend lines, avg decision time per verifier, engine-failure rate. |
| **Analytics accuracy %** | Team / verifier / system accuracy — needs a signed-off definition (engine verdict band vs. officer decision agreement) before it ships. |
| **Async screening** | If the model gets slow, `Submit` returns `202` with `status=processing` immediately and a worker calls the engine; the lifecycle diagram already accommodates this. |

---

## 9. Testing Strategy (per guide §15)

| Layer | Type | Stubs | Priority scenarios |
|---|---|---|---|
| Repositories | real MongoDB (`RequireMongo`) | none | create+find, not-found mapping, cursor pagination across pages, `SetDecision` conditional-write race, `NextSequence` monotonicity |
| Services | real repos + a real `FileStore` (local, rooted at `t.TempDir()`) | `screening.MockEngine` only | `Submit` → completed (verdict/risk/reference persisted); `Submit` with engine down → `status=failed` persisted, request still succeeds; `INVALID_DOC_TYPE`; `Decide` once → then `ALREADY_DECIDED`; `INVALID_DECISION`; auth login success / wrong-password / unknown-user indistinguishable / refresh |
| Engine client | `httptest.Server` | the server itself | happy path (verdict + evidence passthrough), non-200 → `UNAVAILABLE`, bad JSON → `BAD_RESPONSE`, API-key header sent |

No test mocks a repository or a service. MongoDB comes from `docker compose up`.

---

## 10. Build Order

1. **Skeleton** — config, apperr, response, logger, jwt, database + `EnsureIndexes`,
   middleware, `main.go`, Docker. *(done)*
2. **Auth + Users** — login, refresh, RBAC, admin-seeded accounts. *(done)*
3. **Screenings vertical slice** — model → repo → service → handlers: submit (local
   disk / GridFS + engine call + persist), list, get, image, decision; audit logging.
   *(done)*
4. **Three-role model** — `verifier` / `admin` / `superadmin`; admin routes accept
   admin + superadmin; bootstrap seed is a superadmin. *(done)*
5. **Blacklist** module — document/identity entries, admin CRUD + deactivate, verifier
   `check`. *(done — screening-flow wiring is the remaining follow-up, §8)*
6. **Checkpoints** registry.
7. **Face Verification** module (PS Module 4) behind a `FaceEngine` interface.
8. **Blacklist ↔ Submit wiring** + expiry check + risk contribution.
9. **Cases** (multiple-identity linking) + **Audit read API** + **Dashboard**.
10. Point `SCREENING_ENGINE=http` at the deployed FastAPI model; load-test; deploy.

Each step = model → repository (+ real-DB tests) → service (+ tests, engine stubbed) →
handler → route registration, per `BACKEND_GUIDE.md` §18.
