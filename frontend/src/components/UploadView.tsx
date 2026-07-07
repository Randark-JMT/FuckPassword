import { useEffect, useRef, useState } from "react";
import { getUploadStatus, type UploadStatus } from "../api";

type UiPhase = "idle" | "uploading" | "processing" | "done" | "error";

const POLL_INTERVAL_MS = 500;
const MAX_CONSECUTIVE_ERRORS = 5;

export default function UploadView() {
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [uiPhase, setUiPhase] = useState<UiPhase>("idle");
  const [byteFrac, setByteFrac] = useState(0);
  const [status, setStatus] = useState<UploadStatus | null>(null);
  const [result, setResult] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const pollRef = useRef<number | null>(null);

  function stopPolling() {
    if (pollRef.current !== null) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }

  useEffect(() => () => stopPolling(), []);

  function finish(done: boolean, msg: string) {
    stopPolling();
    if (done) {
      setResult(msg);
      setUiPhase("done");
      setFile(null);
      if (inputRef.current) inputRef.current.value = "";
    } else {
      setError(msg);
      setUiPhase("error");
    }
    setBusy(false);
  }

  function startPolling() {
    let consecutiveErrors = 0;
    stopPolling();
    pollRef.current = window.setInterval(async () => {
      try {
        const s = await getUploadStatus();
        consecutiveErrors = 0;
        setStatus(s);
        if (s.phase === "done") {
          finish(
            true,
            `Inserted ${s.inserted.toLocaleString()} new unique record(s)` +
              (s.skipped > 0 ? `; dropped ${s.skipped.toLocaleString()} overlong line(s).` : ".")
          );
        } else if (s.phase === "error") {
          finish(false, s.error || "processing failed");
        }
      } catch {
        consecutiveErrors++;
        if (consecutiveErrors >= MAX_CONSECUTIVE_ERRORS) {
          finish(false, "lost contact with server during processing");
        }
      }
    }, POLL_INTERVAL_MS);
  }

  function handleUpload() {
    if (!file) return;
    setBusy(true);
    setUiPhase("uploading");
    setByteFrac(0);
    setStatus(null);
    setResult(null);
    setError(null);

    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/upload");
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) setByteFrac(e.loaded / e.total);
    };
    xhr.onload = () => {
      if (xhr.status === 202) {
        setUiPhase("processing");
        startPolling();
        return;
      }
      let msg = xhr.statusText || `HTTP ${xhr.status}`;
      try {
        const body = JSON.parse(xhr.responseText);
        if (body?.error) msg = body.error;
      } catch {
        /* keep statusText */
      }
      finish(false, msg);
    };
    xhr.onerror = () => finish(false, "network error");
    xhr.send(file);
  }

  return (
    <div className="panel">
      <p className="muted">
        Upload a plain-text file (one record per line). Uploads are serialized — the system
        rejects a new upload while one is in progress. Files can be up to several GB.
      </p>
      <div className="row">
        <input
          ref={inputRef}
          type="file"
          accept=".txt,text/plain"
          disabled={busy}
          onChange={(e) => setFile(e.target.files?.[0] ?? null)}
        />
        <div className="grow" />
        <button className="primary" disabled={!file || busy} onClick={handleUpload}>
          {busy ? "Uploading…" : "Upload"}
        </button>
      </div>

      {uiPhase === "uploading" && (
        <div style={{ marginTop: 12 }}>
          <div style={{ background: "var(--panel-2)", borderRadius: 6, overflow: "hidden" }}>
            <div
              style={{
                width: `${byteFrac * 100}%`,
                height: 8,
                background: "var(--accent)",
                transition: "width 0.2s",
              }}
            />
          </div>
          <div className="muted" style={{ marginTop: 4 }}>
            {(byteFrac * 100).toFixed(1)}%
            {byteFrac >= 0.999 ? " — saving Source File…" : ""}
          </div>
        </div>
      )}

      {uiPhase === "processing" && (
        <div className="muted" style={{ marginTop: 12 }}>
          <span className="spinner" style={{ marginRight: 8 }} />
          Processing records…
          {status && (
            <div style={{ marginTop: 4 }}>
              Inserted {status.inserted.toLocaleString()} · Dropped {status.skipped.toLocaleString()}
              {" · Processed "}
              {status.lines_processed.toLocaleString()} line(s)
            </div>
          )}
        </div>
      )}

      {result && <div className="ok" style={{ marginTop: 12 }}>{result}</div>}
      {error && <div className="error" style={{ marginTop: 12 }}>{error}</div>}
    </div>
  );
}
