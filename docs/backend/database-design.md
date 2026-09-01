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
| `users` | Supervisor / admin accounts | `UserRepository` |
| `screenings` | One document-screening case + engine result + officer decision | `ScreeningRepository` |
| `audit_logs` | Append-only trail of every state-changing action | `AuditRepository` (Insert only) |
| `counters` | Atomic per-day sequence for `screenings.reference_no` | `ScreeningRepository.NextSequence` |
| `fs.files` / `fs.chunks` | GridFS — stored document images | `storage.gridFSStore` |

Planned (see `backend-architecture.md` §8): `checkpoints`, `watchlist`, `cases`,
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
  "role":          "supervisor",            // supervisor | admin
  "status":        "active",                // active | disabled
  "created_at":    ISODate,
  "updated_at":    ISODate
}
```

- Two roles only: `supervisor` (works the checkpoint — submits screenings, records
  decisions) and `admin` (manages accounts). The `officer_*` field names on
  `screenings` refer to the acting `supervisor` — "officer" is the job, not a role.
- `UserView` (API) drops `password_hash` and `updated_at`.
- First boot seeds one `admin` (`ADMIN_USERNAME` / `ADMIN_PASSWORD` / `ADMIN_EMAIL`)
  **only while the collection is empty**. Change the password immediately.
- Disable, don't delete (`status: "disabled"`) — `audit_logs` and `screenings` reference
  `user_id` values by hex string.

**Indexes:** `{username: 1}` unique · `{email: 1}` unique.

---

## 3. `screenings`

```jsonc
{
  "_id":            ObjectId,
  "reference_no":   "SCR-20260901-00001",   // unique, gapless per day (see §5)
  "checkpoint_id":  "CP-DEL-T3",            // free string today; FK to `checkpoints` later
  "officer_id":     "66d4...",              // users._id hex of the submitter

  "doc_type":       "passport",             // passport | visa | national_id | driving_license | permit
  "image_file_id":  ObjectId,               // GridFS fs.files._id
  "image_name":     "passport_front.jpg",

  "submitted_number": "Z1234567",           // optional, what the officer typed
  "mrz_line1":        "P<INDDOE<<JANE<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<",   // optional
  "mrz_line2":        "Z1234567<1IND8501011F3001018<<<<<<<<<<<<<<02",   // optional

  "status":         "completed",            // processing | completed | failed
  "verdict":        "SUSPICIOUS",           // GENUINE | SUSPICIOUS | FAKE | INSUFFICIENT_IMAGE_QUALITY | PENDING
  "risk_score":     0.42,                   // 0.0–1.0, mirrors engine.risk_score
  "failure_reason": "",                     // set only when status == failed

  "engine": {                               // the external model's output, stored verbatim
    "verdict":     "SUSPICIOUS",
    "risk_score":  0.42,
    "reasons":     ["MRZ checksum uncertain", "Elevated ELA compression variance"],
    "extracted_fields": { "passport_number": "Z1234567", "surname": "DOE", "given_name": "JANE" },
    "evidence": {                           // predict_pipeline.py's evidence_table, unmodified
      "quality_assessment": { "is_sufficient": true, "blur_score": 142.7 },
      "cnn_score": 0.61,
      "ela_forensics": { "ela_variance": 210.4 },
      "mrz_checksums": { "valid_mrz": false, "status": "MRZ_INVALID_OR_OCR_UNCERTAIN" }
    }
  },

  "officer_decision": {                     // absent until an officer decides; then written once
    "decision":   "refer",                  // clear | refer | detain
    "reason":     "MRZ mismatch, sent to secondary inspection",
    "decided_by": "66d4...",                // users._id hex
    "decided_at": ISODate
  },

  "created_at": ISODate,
  "updated_at": ISODate
}
```

### Design decisions

- **Engine output nested, not flattened.** `engine.evidence` is the model's
  `evidence_table` stored exactly as received — full explainability for disputes and
  intelligence analysis. The top-level `verdict` / `risk_score` are denormalised copies
  for cheap filtering and indexing.
- **`verdict: "PENDING"`** while `status` is `processing` or `failed` — the document
  always has a `verdict` field, so list filters never have to special-case its absence.
- **Engine failure is a persisted state, not a lost request.** `status: "failed"` +
  `failure_reason` — the officer still has the image and can `detain`/`refer`.
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

**Indexes:**
`{reference_no: 1}` unique ·
`{status: 1, created_at: -1}` (dashboard / processing queue) ·
`{verdict: 1}` (filter) ·
`{checkpoint_id: 1, created_at: -1}` (per-checkpoint history) ·
`{officer_id: 1}` (an officer's own submissions).
List pagination uses the implicit `_id` order (`{_id: -1}`, `_id < cursor`).

---

## 4. `audit_logs`

```jsonc
{
  "_id":            ObjectId,
  "user_id":        "66d4...",              // actor, users._id hex ("" for system actions)
  "action":         "screening.decided",    // user.created | screening.submitted | screening.decided
  "reference_type": "screening",            // screening | user
  "reference_id":   "66d4...",
  "old_data":       { "verdict": "SUSPICIOUS", "risk_score": 0.42 },   // optional
  "new_data":       { "decision": "refer", "reason": "..." },          // optional
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

## 5. `counters` — gapless `reference_no`

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

## 6. GridFS — `fs.files` / `fs.chunks`

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

## 7. Index Creation

All indexes are declared once, in `internal/database/mongo.go` → `EnsureIndexes`, and
created on every boot (idempotent):

```go
db.Collection(model.CollUsers).Indexes().CreateMany(ctx, []mongo.IndexModel{
    {Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_users_username")},
    {Keys: bson.D{{Key: "email",    Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_users_email")},
})
// ... screenings, audit_logs ...
```

Adding a collection or an access pattern = add its `mongo.IndexModel` here. The
`counters` and GridFS collections need no explicit index beyond their `_id`.

---

## 8. Relationships

```
users (1) ────< screenings.officer_id            (hex string, not a DB ref)
users (1) ────< screenings.officer_decision.decided_by
users (1) ────< audit_logs.user_id
screenings (1) ──1 fs.files                       via image_file_id (GridFS)
screenings (1) ──< audit_logs                     via reference_type="screening" + reference_id
counters (1 per day) ──< screenings               via reference_no allocation (no stored ref)
```

There are no enforced foreign keys — MongoDB has none, and the app treats a dangling
`officer_id` (a disabled/removed user) as a display concern, not an integrity failure.
The invariants that matter (one decision per screening, unique `reference_no`, unique
`username`/`email`) are enforced by unique indexes and conditional updates.
