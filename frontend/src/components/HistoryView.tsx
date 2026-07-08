import { useEffect, useState } from "react";
import { getJobHistory, type Job } from "../api";

const PAGE_SIZE = 50;

function formatDate(iso: string | null | undefined): string {
  if (!iso) return "-";
  return new Date(iso).toLocaleString([], {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

function runtime(job: Job): string {
  if (!job.started_at) return "-";
  const end = job.finished_at ? new Date(job.finished_at).getTime() : Date.now();
  const ms = Math.max(0, end - new Date(job.started_at).getTime());
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

function matchText(job: Job): string {
  if (job.status === "completed") return `${(job.match_count ?? 0).toLocaleString()} match(es)`;
  if (job.status === "failed" && job.error) return job.error;
  return "-";
}

export default function HistoryView({ onFocus }: { onFocus: (id: string) => void }) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [loading, setLoading] = useState(false);
  const [hasMore, setHasMore] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function load(reset: boolean) {
    setLoading(true);
    setError(null);
    try {
      const offset = reset ? 0 : jobs.length;
      const res = await getJobHistory(offset, PAGE_SIZE);
      setJobs((prev) => (reset ? res.jobs : [...prev, ...res.jobs]));
      setHasMore(res.count === res.limit);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load(true);
  }, []);

  return (
    <div className="panel history-panel">
      <div className="row history-toolbar">
        <h2 style={{ margin: 0 }}>Query History</h2>
        <span className="muted">{jobs.length.toLocaleString()} loaded</span>
        <div className="grow" />
        <button disabled={loading} onClick={() => load(true)}>
          refresh
        </button>
      </div>

      {error && <div className="error" style={{ marginBottom: 12 }}>{error}</div>}

      <div className="history-list">
        <div className="history-row history-head">
          <span>#</span>
          <span>Status</span>
          <span>Pattern</span>
          <span>Mode</span>
          <span>Created</span>
          <span>Runtime</span>
          <span>Result</span>
          <span />
        </div>
        {jobs.length === 0 ? (
          <div className="muted history-empty">No query history yet.</div>
        ) : (
          jobs.map((job) => (
            <div className="history-row" key={job.id}>
              <span className="mono muted">{job.position}</span>
              <span><span className={`pill ${job.status}`}>{job.status}</span></span>
              <span className="history-pattern" title={job.pattern}>{job.pattern}</span>
              <span className="muted">{job.is_regex ? "regex" : "substring"}</span>
              <span className="muted">{formatDate(job.created_at)}</span>
              <span className="muted">{runtime(job)}</span>
              <span className={job.status === "failed" ? "error history-result" : "muted history-result"} title={matchText(job)}>
                {matchText(job)}
              </span>
              <span className="history-actions">
                <button onClick={() => onFocus(job.id)}>
                  {job.status === "completed" ? "view" : "details"}
                </button>
              </span>
            </div>
          ))
        )}
      </div>

      {hasMore && jobs.length > 0 && (
        <div className="row" style={{ marginTop: 12, justifyContent: "center" }}>
          <button disabled={loading} onClick={() => load(false)}>
            {loading ? "loading..." : "load older"}
          </button>
        </div>
      )}
    </div>
  );
}
