# Indexing strategy for pathological line lengths

Records are deduplicated via a fixed-size **SHA-256 hash column** (`text_hash`, generated
and stored), with the UNIQUE index on the hash rather than on the raw text. Search uses a
**GIN trigram index on `text`**, and the ingest layer **drops any line longer than
`MAX_LINE_BYTES`** (default 4096) before it reaches the table.

The trigger: real credential records are ~45 bytes, but the dataset contains at least one
line of ~453 KB (a blob with no newline, or a corrupted run). Postgres indexes are bounded
by page size — a B-tree index entry maxes out near ~2.7 KB and a GIN entry near ~8 KB — so
both a raw-text UNIQUE index and a GIN trigram index over unbounded text fail with
`index row requires N bytes, maximum size is 8191` (SQLSTATE 54000). Postgres *stores* a
453 KB `text` value fine; only the indexes choke.

The dedup fix is forced: a hash column is fixed-size and always fits, so dedup is robust to
any line length. For search, the choice was how to keep the GIN trigram index (which gives
sub-second common queries) without it ever seeing an oversized value. **Dropping overlong
lines at ingest** guarantees every indexed value is ≤ 4096 bytes, so the GIN index is always
safe and common queries stay fast. The cost — anomalous blobs are discarded wholesale rather
than stored — was accepted over the alternatives: verbatim storage + seq-scan-everything
(every query scans the full corpus, routinely tens of seconds, eating the 60s budget on
large data), and cap-and-truncate (keeps the line but lies about its contents).

The cap is configurable (`MAX_LINE_BYTES`) so it can be tuned; the default is generous
relative to real credential lines (~90×) and conservative relative to the GIN page limit.
