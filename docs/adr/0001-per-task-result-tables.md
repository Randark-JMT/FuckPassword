# Per-task result tables

Query Job results are stored in a dedicated table per job, named `result_<task_id>`, rather than in a single shared `query_results` table keyed by `job_id`. Each Result Table is dropped when its job expires (7-day TTL).

Result sets can be large — a regex may match millions of Records. A per-job table makes expiry an instant `DROP TABLE` with no dead-tuple bloat (a `DELETE WHERE job_id = ...` on a shared table would leave millions of dead tuples to vacuum), keeps each job's results isolated, and lets the streamed download be a direct `COPY result_<task_id> TO ...`.

The trade-off is runtime DDL and many small tables — acceptable here because job throughput is low and the Queue serializes work, so table churn is modest.

`task_id` is a UUID and table names are built by interpolating a strictly-validated UUID only, never raw input — the dynamic table name cannot become a SQL-injection vector. Each Result Table stores `record_id` referencing the Dataset, not the full Record text, so result tables stay thin and credential text is never duplicated into them.
