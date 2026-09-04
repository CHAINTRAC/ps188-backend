# PS188 — Database Design (MongoDB)

Data model for the PS188 AI-Based Fake Identity & Document Screening System.

**One MongoDB database** (`MONGO_DB`, default `ps188`), single deployment, role-based
access — no multi-tenancy. Document images are stored in **GridFS** inside the same
database. Schema is enforced by the Go structs in `internal/model/` plus the indexes
in `internal/database/mongo.go` (`EnsureIndexes`) — there is no ORM and no migration
tool.

Companion: `backend-architecture.md` (modules, endpoints, lifecycle),
`BACKEND_GUIDE.md` (how models/repositories are written).

---

## 1. Collections Overview

| Collection | Purpose | Written by |
|---|---|---|
| `users` | verifier / admin / superadmin accounts | `UserRepository` |
| `checkpoints` | Checkpoint registry — code, region, managing admin, status | `CheckpointRepository` |
| `screenings` | One document-screening case + engine result + officer decision | `ScreeningRepository` |
| `blacklist` | Blacklisted document numbers and identities | `BlacklistRepository` |
| `audit_logs` | Append-only trail of every state-changing action | `AuditRepository` (Insert only) |
| `counters` | Atomic per-day sequence for `screenings.reference_no` | `ScreeningRepository.NextSequence` |
| `fs.files` / `fs.chunks` | GridFS — stored document images | `storage.gridFSStore` |

Planned (see `backend-architecture.md` §8): `cases`,
`face_verifications` (or embedded), `internal_notifications`.

---

## 2. `users`

```jsonc
{
  "_id":           ObjectId,
  "username":      "sup.jane",              // unique, lowercased
  "full_name":     "Jane Doe",
  "email":         "jane@ps188.local",      // unique, lowercased
  "password_hash": "$2a$12$...",            // bcrypt cost 12 — never returned by any API
  "role":          "verifier",              // verifier | admin | superadmin
  "status":        "active",                // active | disabled
  "region":        "north",                 // admin: assigned region; verifier: inherited from checkpoint; superadmin: "" (all)
  "checkpoint_id": "CP-04",                 // verifier only — the checkpoint code they work
  "created_at":    ISODate,
  "updated_at":    ISODate
}
```

- Three roles: `verifier` (works the checkpoint — submits screenings, records
  decisions), `admin` (manages verifier accounts + the blacklist for one region),
  `superadmin` (manages admins and checkpoints, org-wide). Admin-level routes accept
  both `admin` and `superadmin`. The `officer_*` field names on `screenings` refer to
  the acting `verifier` — "officer" is the job, not a role.
- **Region scope.** A verifier's `region` is resolved from its `checkpoint_id`
  against the `checkpoints` registry at create time — never entered directly. An
  admin's `region` is required and must already exist in the registry. A superadmin
  has both empty, meaning org-wide. `TokenData` carries `region` + `checkpoint_id`
  so request scoping needs no per-call user lookup (re-issued on refresh).
- `UserView` (API) drops `password_hash` and `updated_at`; exposes `region` +
  `checkpoint_id`.
- First boot seeds one `superadmin` (`SUPERADMIN_USERNAME` / `SUPERADMIN_PASSWORD` /
  `SUPERADMIN_EMAIL`, legacy `ADMIN_*` still read as a fallback) **only while the
  collection is empty**. Change the password immediately.
- Disable, don't delete (`status: "disabled"`) — `audit_logs` and `screenings` reference
  `user_id` values by hex string.
- Passwords: `POST /api/users/change-password` (self, verifies current) and
  `POST /api/users/:id/reset-password` (admin → verifier in region, superadmin →
  anyone; returns a temp password). Both audited.

**Indexes:** `{username: 1}` unique · `{email: 1}` unique · `{role: 1, region: 1}`.

---

## 2b. `checkpoints`

```jsonc
{
  "_id":        ObjectId,
  "code":       "CP-04",                    // unique, upper-cased — external id used in URLs and on users/screenings
  "region":     "north",                    // authoritative source of a verifier's region
  "admin_id":   "66d4...",                  // users._id hex of the managing admin (optional)
  "status":     "active",                   // active | attention
  "created_at": ISODate,
  "updated_at": ISODate
}
```

- Super-admin CRUD: `POST /api/checkpoints`, `PATCH /api/checkpoints/:code`
  (assign admin / flip status). `GET /api/checkpoints` returns an admin's own
  region only, a superadmin's everything. Audited as `checkpoint.created` /
  `checkpoint.updated`.
- `code` and `region` are immutable after creation; only `admin_id` and `status`
  can be patched.
- `CheckpointService.Resolve(code) → region` is used by user create (verifier) and,
  later, screening submit to denormalise the region.

**Indexes:** `{code: 1}` unique · `{region: 1}` · `{admin_id: 1}`.

---

## 3. `screenings`

```jsonc
{
  "_id":            ObjectId,
  "reference_no":   "SCR-20260901-00001",   // unique, gapless per day (see §5)
  "checkpoint_id":  "CP-04",                // the officer's checkpoint code (from their token)
  "region":         "north",                // denormalised from the checkpoint at submit time — powers admin scoping
  "officer_id":     "66d4...",              // users._id hex of the submitter

  "doc_type":       "passport",             // passport | visa | national_id | driving_license | permit
  "image_file_id":  ObjectId,               // GridFS fs.files._id
  "image_name":     "passport_front.jpg",
  "flags":          ["blacklist_hit", "expired_document"],  // advisory markers — never auto-blocking
  "blacklist_matches": [                    // present when flags contains "blacklist_hit"
    { "entry_id": "66d5...", "kind": "document", "doc_number": "Z1234567",
      "reason": "reported stolen — Interpol SLTD", "source": "Interpol SLTD" }
  ],

  "submitted_number": "Z1234567",           // optional, what the officer typed
  "mrz_line1":        "P<INDDOE<<JANE<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<",   // optional
  "mrz_line2":        "Z1234567<1IND8501011F3001018<<<<<<<<<<<<<<02",   // optional

  "status":         "completed",            // processing | completed | failed
  "verdict":        "SUSPICIOUS",           // GENUINE | SUSPICIOUS | FAKE | INSUFFICIENT_IMAGE_QUALITY | PENDING
  "risk_score":     0.42,                   // STORED 0.0–1.0 — API projections convert to 0–100
  "failure_reason": "",                     // set only when status == failed

  "engine": {                               // the external model's output
    "verdict":     "SUSPICIOUS",
    "risk_score":  0.42,                    // stored 0.0–1.0
    "reasons":     ["MRZ checksum uncertain", "Elevated ELA compression variance"],
    "extracted_fields": [                   // []ExtractedField — per-field OCR confidence
      { "label": "document_number", "value": "Z1234567", "confidence": 0.97 },
      { "label": "surname", "value": "DOE", "confidence": 0.9 }
    ],
    "evidence_items": [                      // []EvidenceItem — toned lines for the UI (json key: "evidence")
      { "tone": "warn", "text": "MRZ checksum uncertain" }
    ],
    "evidence": {                           // RawEvidence — predict_pipeline.py's evidence_table, unmodified (json key: "raw_evidence")
      "quality_assessment": { "is_sufficient": true, "blur_score": 142.7 },
      "cnn_score": 0.61,
      "ela_forensics": { "ela_variance": 210.4 },
      "mrz_checksums": { "valid_mrz": false, "status": "MRZ_INVALID_OR_OCR_UNCERTAIN" }
    }
  },

  "officer_decision": {                     // absent until an officer decides; then written once
    "decision":   "escalate",               // accept | escalate | reject
    "reason":     "MRZ mismatch, sent to secondary inspection",
    "decided_by": "66d4...",                // users._id hex
    "decided_at": ISODate
  },

  "created_at": ISODate,
  "updated_at": ISODate
}
```

### Design decisions

- **Engine output nested, not flattened.** `engine.evidence` (`RawEvidence`) is the
  model's `evidence_table` stored exactly as received — full explainability for
  disputes. `engine.extracted_fields` (`[]ExtractedField` with per-field confidence)
  and `engine.evidence_items` (`[]EvidenceItem` with `good|warn|bad` tone) are the
  structured forms the UI renders; when the model omits them the backend derives
  `evidence_items` from `reasons` + risk band and leaves `extracted_fields` empty.
  The top-level `verdict` / `risk_score` are denormalised copies for cheap filtering.
- **Risk score: stored `0.0–1.0`, served `0–100`.** Every document keeps the model's
  native float. `model.riskTo100` (the single conversion point) turns it into an
  integer `0–100` in `ScreeningView` and `EngineView` — what the UI `RiskGauge` and
  history expect. `verdict_band` (`Verdict.Band()`) is also exposed for 3-state UI
  controls; `INSUFFICIENT_IMAGE_QUALITY` / `PENDING` band to `SUSPICIOUS`.
- **`verdict: "PENDING"`** while `status` is `processing` or `failed` — the document
  always has a `verdict` field, so list filters never have to special-case its absence.
- **Engine failure is a persisted state, not a lost request.** `status: "failed"` +
  `failure_reason` — the officer still has the image and can `reject`/`escalate`.
- **`officer_decision` embedded, written once.** One decision per screening, so a
  sub-document (not a separate collection). Enforced by a conditional update:
  `{ _id, officer_decision: { $exists: false } }`. A losing concurrent writer matches
  zero documents → `ALREADY_DECIDED`.
- **`reference_no` snapshot.** Human-facing, printed on any checkpoint record; generated
  from an atomic counter (§5), never derived from `MAX()` over the collection.
- **`officer_id` / `decided_by` are hex strings, not `ObjectId` refs.** They come out of
  the JWT as strings; no `$lookup` is done on the hot path. `users` is small — join in
  the app if a screening list ever needs officer names.
- **Images in GridFS.** Same database as everything else → one backup, one connection,
  no shared-volume problem across replicas. `image_file_id` is the `fs.files._id`.

- **`region` denormalised, not looked up.** Copied from the officer's checkpoint
  (carried on the JWT) when the screening is created. The list endpoint scopes a
  verifier to their own `officer_id`, an admin to their own `region`, and a super
  admin to nothing — `middleware.ScopeToActor` overrides any client-supplied
  `officer_id` / `region` query param.
- **`flags[]` + `blacklist_matches[]` are advisory.** After the engine call,
  `ScreeningService.Submit` runs `BlacklistService.Check` (submitted / extracted
  document number + identity) and an expiry check on the submitted / extracted
  expiry date. A hit raises `blacklist_hit` (with `blacklist_matches[]` and an
  appended `engine.reasons` note) and bumps `risk_score` by `0.25`; a past expiry
  raises `expired_document` and bumps by `0.15` (both clamped to `1.0`). The
  verdict is **never** changed and the screening **never** auto-blocks — the
  officer still records the decision. `face_mismatch` / `multiple_identity` are
  reserved for later modules.

**Indexes:**
`{reference_no: 1}` unique ·
`{status: 1, created_at: -1}` (dashboard / processing queue) ·
`{verdict: 1}` (filter) ·
`{checkpoint_id: 1, created_at: -1}` (per-checkpoint history) ·
`{region: 1, created_at: -1}` (admin region history) ·
`{officer_id: 1, created_at: -1}` (an officer's own submissions / "My History").
List pagination uses the implicit `_id` order (`{_id: -1}`, `_id < cursor`).

---

## 4. `blacklist`

```jsonc
{
  "_id":         ObjectId,
  "kind":        "document",                // document | identity
  "doc_number":  "Z1234567",                // kind == document — upper-cased, trimmed
  "name":        "JOHN DOE",                // kind == identity  — upper-cased, trimmed
  "dob":         "1985-01-01",              // kind == identity, optional — ISO date
  "nationality": "IND",                     // kind == identity, optional — ISO-3, upper-cased
  "reason":      "reported stolen — Interpol SLTD",
  "source":      "Interpol SLTD",           // optional free text
  "added_by":    "66d4...",                 // users._id hex
  "active":      true,
  "created_at":  ISODate,
  "updated_at":  ISODate
}
```

- Covers the PS lines "expired or blacklisted travel documents" and "multiple
  identities used by the same person". Admin- and superadmin-managed.
- **Two match kinds.** `document` matches a single document number; `identity`
  matches a person by name, optionally narrowed by date of birth and nationality.
  A blank stored `dob` / `nationality` on an identity entry still matches (name-only
  blacklisting).
- Lookups normalise the probe (upper-case + trim) so matching is case- and
  whitespace-insensitive. `BlacklistService.Check` returns every active match; it
  **never blocks** — the officer still decides.
- **Never hard-deleted.** `POST /api/blacklist/:id/deactivate` flips `active` to
  `false` so the trail survives. Adding a second active entry that matches an existing
  one returns `BLACKLIST_ENTRY_EXISTS` (70002).
- Audit: `blacklist.added`, `blacklist.deactivated`.

**Indexes:** `{doc_number: 1, active: 1}` · `{name: 1, dob: 1, nationality: 1, active: 1}`
· `{kind: 1, active: 1}`.

---

## 5. `audit_logs`

```jsonc
{
  "_id":            ObjectId,
  "user_id":        "66d4...",              // actor, users._id hex ("" for system actions)
  "action":         "screening.decided",    // auth.login | user.created | user.password_changed | user.password_reset | screening.submitted | screening.decided | blacklist.added | blacklist.deactivated | checkpoint.created | checkpoint.updated
  "reference_type": "screening",            // screening | user | blacklist | checkpoint
  "reference_id":   "66d4...",
  "old_data":       { "verdict": "SUSPICIOUS", "risk_score": 0.42 },   // optional
  "new_data":       { "decision": "escalate", "reason": "...", "detail": "screening.decided · ESCALATE" },  // optional
  "ip_address":     "10.12.4.9",
  "created_at":     ISODate
}
```

- **Append-only.** `AuditRepository` exposes `Insert` and nothing else — no update, no
  delete. This is the "digital trail for investigations" the PS calls for.
- Written by services as a **best-effort side effect** (`_ = audit.Insert(...)`) — an
  audit-write failure never rolls back or fails the underlying operation.
- `old_data` / `new_data` are free-form `bson.M` snapshots of the changed state.

**Indexes:** `{created_at: -1}` · `{reference_type: 1, reference_id: 1}`.

---

## 6. `counters` — gapless `reference_no`

```jsonc
{ "_id": "screening:20260901", "seq": 42 }
```

`ScreeningRepository.NextSequence(ctx, "20260901")` does:

```
FindOneAndUpdate(
  { _id: "screening:" + day },
  { $inc: { seq: 1 } },
  { upsert: true, returnDocument: After }
) → seq
```

The returned `seq` becomes `SCR-<day>-<seq zero-padded to 5>`. The `$inc` is atomic, so
concurrent submissions serialise correctly and never collide. If a submission fails
*after* allocating a number the number is skipped (rare, acceptable) — but two
submissions never get the same one.

> A per-*day* key keeps the counter document tiny and contention low. A new day's first
> screening upserts a fresh counter document at `seq: 1`.

---

## 7. GridFS — `fs.files` / `fs.chunks`

Default bucket (`db.GridFSBucket()`), managed entirely by `internal/storage/gridfs.go`:

- `Put(ctx, filename, reader) → hex id` — `UploadFromStream`.
- `Get(ctx, id, writer)` — `DownloadToStream`.
- `Delete(ctx, id)` — `Delete`; a missing file is not an error (used to roll back a
  half-created screening).

Only JPEG and PNG are accepted (`http.DetectContentType` check in the handler), capped
at `MAX_UPLOAD_BYTES` (default 10 MiB). The screening document keeps the `fs.files._id`
in `image_file_id`; `GET /api/screenings/:id/image` streams it back with a sniffed
`Content-Type`.

---

## 8. Index Creation

All indexes are declared once, in `internal/database/mongo.go` → `EnsureIndexes`, and
created on every boot (idempotent):

```go
db.Collection(model.CollUsers).Indexes().CreateMany(ctx, []mongo.IndexModel{
    {Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_users_username")},
    {Keys: bson.D{{Key: "email",    Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_users_email")},
})
// ... checkpoints, screenings, blacklist, audit_logs ...
```

`checkpoints`: `{code: 1}` unique · `{region: 1}` · `{admin_id: 1}`.
`users` also carries `{role: 1, region: 1}` for region-scoped account lists.

Adding a collection or an access pattern = add its `mongo.IndexModel` here. The
`counters` and GridFS collections need no explicit index beyond their `_id`.

---

## 9. Relationships

```
checkpoints (1) ──< users.checkpoint_id           (verifier — code string; region denormalised onto the user)
checkpoints (1) ──1 users.admin_id                 (managing admin)
users (1) ────< screenings.officer_id            (hex string, not a DB ref)
users (1) ────< screenings.officer_decision.decided_by
users (1) ────< blacklist.added_by
users (1) ────< audit_logs.user_id
screenings (1) ──1 fs.files                       via image_file_id (GridFS)
screenings (1) ──< audit_logs                     via reference_type="screening" + reference_id
counters (1 per day) ──< screenings               via reference_no allocation (no stored ref)
```

There are no enforced foreign keys — MongoDB has none, and the app treats a dangling
`officer_id` (a disabled/removed user) as a display concern, not an integrity failure.
The invariants that matter (one decision per screening, unique `reference_no`, unique
`username`/`email`) are enforced by unique indexes and conditional updates.
