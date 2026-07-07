import { useEffect, useState } from "react";
import { cancelJob, getBoard, type Job } from "../api";

function timeAgo(iso: string | null | undefined): string {
  if (!iso) return "";
  const ms = Date.now() - new Date(iso).getTime();
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  return `${m}m ${s % 60}s`;
}

function JobRow({ job, onFocus }: { job: Job; onFocus: (id: string) => void }) {
  const [cancelling, setCancelling] = useState(false);
  return (
    <div className="job">
      <span className="pos">{job.position}</span>
      <span className="pat" title={job.pattern}>{job.pattern}</span>
      {job.is_regex && <span className="pill queued">regex</span>}
      <span className="meta">
        {job.status === "running" ? `running ${timeAgo(job.started_at)}` : `queued ${timeAgo(job.created_at)}`}
      </span>
      <button onClick={() => onFocus(job.id)}>view</button>
      <button
        className="danger"
        disabled={cancelling}
        onClick={async () => {
          setCancelling(true);
          await cancelJob(job.id);
          setCancelling(false);
        }}
      >
        cancel
      </button>
    </div>
  );
}

export default function StatusBoard({ onFocus }: { onFocus: (id: string) => void }) {
  const [running, setRunning] = useState<Job | null>(null);
  const [queued, setQueued] = useState<Job[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function poll() {
      try {
        const b = await getBoard();
        if (cancelled) return;
        setRunning(b.running);
        setQueued(b.queued);
        setError(null);
      } catch (e) {
        if (!cancelled) setError((e as Error).message);
      }
    }
    poll();
    const t = setInterval(poll, 2000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, []);

  return (
    <div className="panel">
      <div className="row" style={{ marginBottom: 12 }}>
        <h2 style={{ margin: 0 }}>Status Board</h2>
        <span className="muted">auto-refreshing every 2s</span>
      </div>

      {error && <div className="error">{error}</div>}

      <label>Currently executing</label>
      {running ? (
        <JobRow job={{ ...running, status: "running" }} onFocus={onFocus} />
      ) : (
        <p className="muted">— idle —</p>
      )}

      <label style={{ marginTop: 16 }}>
        Waiting queue ({queued.length})
      </label>
      {queued.length === 0 ? (
        <p className="muted">— empty —</p>
      ) : (
        queued.map((j) => <JobRow key={j.id} job={j} onFocus={onFocus} />)
      )}
    </div>
  );
}
