# Single-Column Data Query System

A system that ingests large plain-text datasets — one record per line — and serves substring and regular-expression queries over the full corpus, executing queries one at a time via a queue.

## Language

**Record**:
A single line of ingested text, treated as opaque content. Identity is the exact text — two identical lines are one Record. The unit of matching. Lines longer than the configured byte cap (default 4 KB) are dropped at ingest, never becoming Records.
_Avoid_: line, entry, item, row

**Source File**:
A plain-text file uploaded by a user, containing one Record per line.
_Avoid_: upload, input, txt

**Dataset**:
The set of unique Records across all Source Files — the corpus that queries run against. A Record appears at most once; duplicates are discarded at ingest. The Dataset grows by append across Uploads.
_Avoid_: database, collection, table

**Upload**:
The act of streaming a Source File into the Dataset. Each Upload appends its Records (deduplicated) to the Dataset. Uploads are serialized — the upload function is locked while an Upload is in progress, so a new Upload cannot begin until the current one finishes.
_Avoid_: import, load, ingest

**Query Job**:
A submitted query expression accepted into the Queue for execution, identified by a unique id. Each Query Job runs exactly once and has a pollable status — queued, running, completed, or failed. Its id names its Result Table.
_Avoid_: search, request, task, query (reserve "query" for the expression itself)

**Query Result**:
The set of Records a Query Job matched, materialized in the job's Result Table. The UI displays the first 1,000 Records; the full set is available as a streamed download. Expired after 7 days.
_Avoid_: output, matches, hits

**Result Table**:
A persisted table named `result_<task_id>` holding one Query Job's Query Results, keyed by reference to the matching Record rather than by copied text. Dropped when its job expires.
_Avoid_: temp table, output table

**Queue**:
The single, global, first-in-first-out list of Query Jobs awaiting execution. Exactly one Query Job — the head — executes at any moment; all others wait in arrival order.
_Avoid_: job list, work pool, lane

**Status Board**:
The interface surface that shows the currently-executing Query Job and the Query Jobs waiting behind it, in order.
_Avoid_: dashboard, monitor
