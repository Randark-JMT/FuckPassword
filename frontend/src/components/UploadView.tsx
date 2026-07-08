import { useEffect, useMemo, useRef, useState } from "react";
import { getUploadStatus, type TaskSnapshot, type UploadStatus } from "../api";

const POLL_INTERVAL_MS = 500;
const MAX_CONSECUTIVE_ERRORS = 5;

function pct(done: number, total: number): number | null {
  if (!Number.isFinite(done) || !Number.isFinite(total) || total <= 0) return null;
  return Math.max(0, Math.min(100, (done / total) * 100));
}

function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "unknown";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = n;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function activeUpload(status: UploadStatus | null): boolean {
  return status?.phase === "uploading" || status?.phase === "processing";
}

function taskLabel(task: TaskSnapshot | undefined): string {
  if (!task?.busy) return "";
  if (task.kind === "query") return "A query is running. Upload will be available when it finishes.";
  if (task.kind === "upload") return "An upload is already running.";
  return "Another task is running.";
}

export default function UploadView() {
  const [file, setFile] = useState<File | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [localByteFrac, setLocalByteFrac] = useState(0);
  const [status, setStatus] = useState<UploadStatus | null>(null);
  const [result, setResult] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const lastPhaseRef = useRef<UploadStatus["phase"] | null>(null);

  useEffect(() => {
    let cancelled = false;
    let consecutiveErrors = 0;

    async function poll() {
      try {
        const s = await getUploadStatus();
        if (cancelled) return;
        consecutiveErrors = 0;
        setStatus(s);

        const lastPhase = lastPhaseRef.current;
        if ((submitting || lastPhase === "processing" || lastPhase === "uploading") && s.phase === "done") {
          setResult(
            `Inserted ${s.inserted.toLocaleString()} new unique record(s)` +
              (s.skipped > 0 ? `; dropped ${s.skipped.toLocaleString()} overlong line(s).` : ".")
          );
          setError(null);
          setSubmitting(false);
          setFile(null);
          if (inputRef.current) inputRef.current.value = "";
        } else if ((submitting || lastPhase === "processing" || lastPhase === "uploading") && s.phase === "error") {
          setResult(null);
          setError(s.error || "processing failed");
          setSubmitting(false);
        }
        lastPhaseRef.current = s.phase;
      } catch {
        consecutiveErrors++;
        if (!cancelled && consecutiveErrors >= MAX_CONSECUTIVE_ERRORS) {
          setError("lost contact with server during upload status polling");
          setSubmitting(false);
        }
      }
    }

    poll();
    const t = window.setInterval(poll, POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, [submitting]);

  const uploadActive = activeUpload(status);
  const task = status?.current_task;
  const lockedByOtherTask = Boolean(task?.busy && task.kind !== "upload");
  const controlsDisabled = submitting || uploadActive || lockedByOtherTask;

  const uploadBytePct = useMemo(() => {
    const remote = pct(status?.bytes_received ?? 0, status?.bytes_total ?? 0);
    if (remote === null) return localByteFrac * 100;
    return Math.max(remote, localByteFrac * 100);
  }, [localByteFrac, status?.bytes_received, status?.bytes_total]);

  const linePct = pct(status?.lines_processed ?? 0, status?.lines_total ?? 0);

  function handleUpload() {
    if (!file || controlsDisabled) return;
    setSubmitting(true);
    setLocalByteFrac(0);
    setResult(null);
    setError(null);

    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/upload");
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) setLocalByteFrac(e.loaded / e.total);
    };
    xhr.onload = () => {
      if (xhr.status === 202) return;
      let msg = xhr.statusText || `HTTP ${xhr.status}`;
      try {
        const body = JSON.parse(xhr.responseText);
        if (body?.error) msg = body.error;
      } catch {
        /* keep statusText */
      }
      setSubmitting(false);
      setError(msg);
    };
    xhr.onerror = () => {
      setSubmitting(false);
      setError("network error");
    };
    xhr.send(file);
  }

  return (
    <div className="panel">
      <p className="muted">
        Upload a plain-text file (one record per line). Uploads are serialized with query execution;
        queries may wait in queue, but uploads are never queued.
      </p>

      {uploadActive ? (
        <UploadProgress status={status} bytePct={uploadBytePct} linePct={linePct} />
      ) : (
        <>
          {lockedByOtherTask && <div className="warn" style={{ marginBottom: 12 }}>{taskLabel(task)}</div>}
          <div className="row">
            <input
              ref={inputRef}
              type="file"
              accept=".txt,text/plain"
              disabled={controlsDisabled}
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
            <div className="grow" />
            <button className="primary" disabled={!file || controlsDisabled} onClick={handleUpload}>
              {submitting ? "Uploading..." : "Upload"}
            </button>
          </div>
        </>
      )}

      {!uploadActive && status?.phase === "done" && !result && (
        <div className="ok" style={{ marginTop: 12 }}>
          Last upload inserted {status.inserted.toLocaleString()} new unique record(s)
          {status.skipped > 0 ? `; dropped ${status.skipped.toLocaleString()} overlong line(s).` : "."}
        </div>
      )}
      {!uploadActive && result && <div className="ok" style={{ marginTop: 12 }}>{result}</div>}
      {error && <div className="error" style={{ marginTop: 12 }}>{error}</div>}
    </div>
  );
}

function UploadProgress({
  status,
  bytePct,
  linePct,
}: {
  status: UploadStatus | null;
  bytePct: number;
  linePct: number | null;
}) {
  if (!status) return null;

  if (status.phase === "uploading") {
    return (
      <div className="upload-progress">
        <div className="row progress-head">
          <span className="spinner" />
          <strong>Receiving upload</strong>
          <span className="muted">{bytePct.toFixed(1)}%</span>
        </div>
        <ProgressBar value={bytePct} />
        <div className="muted progress-meta">
          {formatBytes(status.bytes_received)} / {formatBytes(status.bytes_total)} received
        </div>
      </div>
    );
  }

  return (
    <div className="upload-progress">
      <div className="row progress-head">
        <span className="spinner" />
        <strong>Processing records...</strong>
        {linePct !== null && <span className="muted">{linePct.toFixed(1)}%</span>}
      </div>
      <ProgressBar value={linePct ?? 0} indeterminate={linePct === null} />
      <div className="muted progress-meta">
        Inserted {status.inserted.toLocaleString()} · Dropped {status.skipped.toLocaleString()} · Processed{" "}
        {status.lines_processed.toLocaleString()} / {status.lines_total.toLocaleString()} line(s)
      </div>
    </div>
  );
}

function ProgressBar({ value, indeterminate = false }: { value: number; indeterminate?: boolean }) {
  return (
    <div className={`progress-bar${indeterminate ? " indeterminate" : ""}`}>
      <div style={{ width: `${Math.max(0, Math.min(100, value))}%` }} />
    </div>
  );
}
