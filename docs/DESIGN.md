# Design — Single-Column Data Query System

> Build-ready spec. Decisions were stress-tested in a design interview; the load-bearing
> ones are recorded as ADRs in [docs/adr/](docs/adr/) and the shared vocabulary lives in
> [CONTEXT.md](../CONTEXT.md). This document is the consolidated summary.

## 1. Problem

Ingest very large plain-text datasets (one record per line, e.g.
`example.com/Service:admin@test.com:password123`) and serve **substring** and
**regular-expression** queries over the full corpus. Single source files up to ~5 GB; total
corpus ~10 GB+ and growing. Multiple users may submit queries, but execution is serialized
through a queue. Deployed in the client's Docker hosting environment, which already provides
the `pgvector/pgvector:pg16` and `postgres:14` images.

## 2. Architecture

Two containers, no extra infrastructure:

```
                 ┌─────────────────────────────────────────┐
   browser ────► │  app (Go binary: API + embedded React)   │
                 │   - upload handler (streaming)            │
                 │   - query submit / status / results API   │
                 │   - single queue worker (goroutine)       │
                 │   - ingest worker (COPY batches)          │
                 └───────────────┬───────────────────────────┘
                                 │  pgx  (COPY, ~, ILIKE, pg_cancel_backend)
                 ┌───────────────▼───────────────────────────┐
                 │  db: pgvector/pgvector:pg16 (Postgres 16)  │
                 │   - records (GIN trigram)                  │
                 │   - query_jobs                             │
                 │   - result_<task_id> (one per Query Job)   │
                 └────────────────────────────────────────────┘
   volumes:  pg-data (persistent)   upload-staging (.part files)
```

The queue lives **in the database** (`query_jobs` table), not in Redis — a single worker loop
claims the oldest `queued` row, so jobs survive restarts and the Status Board reads the same
table the worker writes.

## 3. Data model

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- The Dataset: one row per unique Record (append-only, deduplicated).
-- Dedup is via a fixed-size SHA-256 hash so the unique index never overflows
-- on long lines (see ADR 0004). Long lines are dropped before insert.
CREATE TABLE records (
    id        bigserial PRIMARY KEY,
    text      text NOT NULL,
    text_hash text GENERATED ALWAYS AS (encode(digest(text, 'sha256'), 'hex')) STORED
);
CREATE UNIQUE INDEX records_text_hash_uq ON records (text_hash);
CREATE INDEX records_text_trgm           ON records USING gin (text gin_trgm_ops);

-- The Queue + job metadata.
CREATE TYPE job_status AS ENUM
    ('queued','running','completed','failed','cancelled');
CREATE TABLE query_jobs (
    id          uuid PRIMARY KEY,            -- the task_id
    pattern     text NOT NULL,
    is_regex    boolean NOT NULL,
    status      job_status NOT NULL DEFAULT 'queued',
    match_count integer,
    error       text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz,
    position    bigint NOT NULL              -- arrival order for FIFO
);

-- One per Query Job, created when the job starts running.
-- FK, not copied text (keeps large result sets small). See ADR 0001.
CREATE TABLE result_<task_id> (
    record_id bigint NOT NULL REFERENCES records(id),
    ord       integer NOT NULL
);
```

`pg_trgm` accelerates both `ILIKE '%...%'` and regex `~` when trigrams can be extracted;
un-indexable patterns fall back to a seq scan (accepted, see Q1).

## 4. Query semantics (Q4)

Single input, one toggle:

| Toggle       | SQL predicate                              | Case       |
|--------------|--------------------------------------------|------------|
| regex **OFF** | `text ILIKE '%' || $1 || '%'`             | insensitive |
| regex **ON**  | `text ~ $1` (match-anywhere, grep-like)   | sensitive (embed `(?i)` to relax) |

Substring mode is case-insensitive; regex mode honors `(?i)` and `\Q…\E`.

## 5. ReDoS & runaway-query guard (Q5, ADR 0002)

Every regex Query Job runs under a hard **`statement_timeout = 60s`** (configurable) on the
worker's session, plus a submission-time cap on pattern length (≤ 500 chars), reject empty.
A runaway pattern is aborted at 60s and marked failed — guaranteeing no single bad pattern
stalls the serialized queue for more than ~60s. RE2 was considered and deferred (not native
to Postgres; the deployment is limited to the two supplied images).

## 6. Upload (Q6, Q7)

**Network:** single streaming `POST /upload` with chunked transfer-encoding; server reads the
body line-by-line. **No resumability** (Option B) — client retries the whole file on failure.

**Ingest (DB-side chunking):**
1. Stream the body to a `.part` file on the staging volume.
2. An ingest worker reads the `.part` in batches of ~50k lines, **drops any line longer
   than `MAX_LINE_BYTES`** (default 4096; counted in a skipped tally — see ADR 0004), `COPY`s
   each batch into a temporary table, then `INSERT INTO records ... ON CONFLICT (text_hash)
   DO NOTHING` (global dedup via the SHA-256 hash). Each batch is idempotent, so re-uploading
   the same file is safe.
3. Delete the `.part` file (staging housekeeping — the Dataset itself is append-only).

The upload response reports both `inserted` (new unique records) and `skipped` (overlong
lines dropped).

**Lock:** the upload function is **locked** while an Upload is in progress; a new upload is
rejected (`409`) until the current one finishes. Queries are **not** blocked during upload
(`COPY` is bulk/row-level; the Dataset remains consistent).

## 7. Queue, Status Board, results (Q1, Q3, Q8)

- **Queue:** global FIFO by `position`. Exactly one job runs at a time. **Max 20 pending**;
  submissions beyond that return `409 queue full`.
- **Cancel:** any visitor may cancel any job (no auth — see ADR 0003). Queued → removed
  instantly; running → `pg_cancel_backend(pid)` aborts the live query, job marked cancelled.
- **Polling (no SSE):** the submitter receives a `task_id` and polls `GET /jobs/:id`.
- **Status Board:** `GET /jobs` → the running job + the ordered waiting queue.
- **Results:** stored in `result_<task_id>` (FK → `records.id`). UI shows the **first 1,000**;
  the **full set is a streamed txt download**. Results and their job row expire after **7 days**.

## 8. Security posture (ADR 0003)

**No authentication, no user identity.** Anyone reachable to the deployment may upload, query,
cancel, view, or download anything; Query Jobs carry no owner. The deployment **must be
network-isolated** (private LAN / VPN / internal subnet) — network-level isolation is the sole
access boundary. Removing it requires reintroducing app-level auth.

## 9. Tech stack (Q9)

| Layer    | Choice |
|----------|--------|
| Database | `pgvector/pgvector:pg16` (Postgres 16; the `vector` ext is unused — it's just pg16) |
| Backend  | Go, `pgx` driver, single binary, embedded static assets |
| Frontend | React + Vite + TypeScript |
| Queue    | in-DB `query_jobs` table + single worker goroutine |

## 10. Delivery (Q10)

- **`Dockerfile`** — multi-stage: build React (`npm run build`), build Go binary embedding the
  frontend (`go build`), copy into a slim runtime image. **amd64 only.**
- **`docker-compose.yml`** (prod) — two services:
  - `db` → `pgvector/pgvector:pg16`, mounted `pg-data` volume.
  - `app` → the prebuilt image from GHCR, env-var DB DSN pointing at `db`, mounted
    `upload-staging` volume, published port.
- **`docker-compose.override.yml`** (dev) — builds `app` from local source instead of pulling.
- **`.github/workflows/build.yml`** — on push to `main` and on version tags: multi-stage build,
  `docker/build-push-action` to **GHCR** tagged `latest` + short-sha + tag. PRs build only
  (no push). Build for `linux/amd64`.

## API surface

| Method & path                    | Purpose |
|----------------------------------|---------|
| `POST /upload`                   | Stream a Source File into the Dataset. `409` if locked. |
| `POST /jobs` `{pattern,is_regex}`| Submit a Query Job. `409` if queue full (>20). Returns `{task_id}`. |
| `GET /jobs`                      | Status Board: running job + ordered waiting queue. |
| `GET /jobs/:id`                  | Job status (and, if completed, summary). |
| `GET /jobs/:id/results`          | First 1,000 matched Records. |
| `GET /jobs/:id/download`         | Full match set, streamed as txt. |
| `POST /jobs/:id/cancel`          | Cancel queued or running job. |

## Decisions recorded as ADRs

- [0001 — Per-Query-Job result tables (FK, not copied text)](docs/adr/0001-per-task-result-tables.md)
- [0002 — Regex safety via statement_timeout (RE2 deferred)](docs/adr/0002-regex-safety-via-statement-timeout.md)
- [0003 — No authentication, no user identity](docs/adr/0003-no-authentication.md)
- [0004 — Indexing strategy for pathological line lengths](docs/adr/0004-indexing-long-lines.md)

Shared vocabulary: [CONTEXT.md](../CONTEXT.md).
