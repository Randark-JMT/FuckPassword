import { useEffect, useState } from "react";
import { downloadURL, getJob, getResults, type Job } from "../api";

export default function ResultsView({ jobId }: { jobId: string }) {
  const [job, setJob] = useState<Job | null>(null);
  const [results, setResults] = useState<string[] | null>(null);
  const [total, setTotal] = useState(0);
  const [truncated, setTruncated] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    let stop = false;

    async function tick() {
      try {
        const j = await getJob(jobId);
        if (cancelled) return;
        setJob(j);
        if (j.status === "completed") {
          stop = true;
          const r = await getResults(jobId, 0, 1000);
          if (cancelled) return;
          setResults(r.results);
          setTotal(r.total);
          setTruncated(r.truncated);
        } else if (j.status === "failed") {
          stop = true;
        } else if (j.status === "cancelled") {
          stop = true;
        }
      } catch (e) {
        if (!cancelled) setError((e as Error).message);
        stop = true;
      }
    }

    tick();
    const t = setInterval(() => {
      if (stop) {
        clearInterval(t);
        return;
      }
      tick();
    }, 1500);

    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, [jobId]);

  if (error) return <div className="error">{error}</div>;
  if (!job) return <div className="muted">loading…</div>;

  const terminal = job.status === "completed" || job.status === "failed" || job.status === "cancelled";

  return (
    <div>
      <div className="row" style={{ marginBottom: 8 }}>
        <span className={`pill ${job.status}`}>{job.status}</span>
        <span className="pat mono" style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {job.pattern}
        </span>
        {job.is_regex && <span className="pill queued">regex</span>}
        {!terminal && <span className="spinner" />}
      </div>

      {job.status === "queued" && <p className="muted">Waiting in queue…</p>}
      {job.status === "running" && <p className="muted">Scanning the dataset…</p>}
      {job.status === "failed" && <div className="error">{job.error}</div>}
      {job.status === "cancelled" && <p className="warn">This job was cancelled.</p>}

      {job.status === "completed" && (
        <>
          <div className="row" style={{ margin: "8px 0" }}>
            <span className="muted">
              {(job.match_count ?? 0).toLocaleString()} match(es)
            </span>
            <div className="grow" />
            <a href={downloadURL(jobId)}>
              <button>Download all</button>
            </a>
          </div>
          {results && results.length > 0 ? (
            <div className="results-list">
              {results.map((r, i) => (
                <div key={i}>{r}</div>
              ))}
            </div>
          ) : (
            <p className="muted">No matches.</p>
          )}
          {truncated && (
            <p className="warn" style={{ marginTop: 8 }}>
              Showing first 1,000 of {total.toLocaleString()}. Use “Download all” for the full set.
            </p>
          )}
        </>
      )}
    </div>
  );
}
