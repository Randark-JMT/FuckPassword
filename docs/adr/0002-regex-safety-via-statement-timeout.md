# Regex safety via statement_timeout (RE2 deferred)

Regex Query Jobs are defended against catastrophic backtracking (ReDoS) by a hard per-query `statement_timeout` on the worker's database session — default 60s, configurable — plus a submission-time cap on pattern length. Runaway patterns are aborted and their Query Job is marked failed.

Because the Queue is serialized (one Query Job runs at a time), a single ReDoS pattern would stall every other user until it finishes — potentially hours. The timeout bounds that stall to ~60s max. Postgres's regex engine is an NFA and is vulnerable to catastrophic backtracking, so some guard is mandatory, not optional.

The alternative — RE2 (linear-time, ReDoS-immune) — was deferred: it is not native to Postgres, the deployment is limited to the two supplied Postgres images, and app-layer RE2 over the deduplicated corpus (tens of millions of rows) is too slow without pre-filtering. If ReDoS incidents occur, revisit by pre-filtering candidates with the trigram index and confirming with RE2 in the application.

The timeout value is easily tuned; the load-bearing decision is to rely on the timeout as the primary defense and to keep the guard mandatory regardless of how benign the queue looks.
