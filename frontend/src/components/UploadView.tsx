import { useRef, useState } from "react";

function uploadWithProgress(
  file: File,
  onProgress: (frac: number) => void
): Promise<{ inserted: number; skipped: number }> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/upload");
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded / e.total);
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText));
        } catch {
          reject(new Error("invalid response"));
        }
      } else {
        try {
          reject(new Error(JSON.parse(xhr.responseText).error || xhr.statusText));
        } catch {
          reject(new Error(xhr.statusText || `HTTP ${xhr.status}`));
        }
      }
    };
    xhr.onerror = () => reject(new Error("network error"));
    xhr.send(file);
  });
}

export default function UploadView() {
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState(0);
  const [result, setResult] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  async function handleUpload() {
    if (!file) return;
    setBusy(true);
    setProgress(0);
    setResult(null);
    setError(null);
    try {
      const res = await uploadWithProgress(file, setProgress);
      setResult(
        `Inserted ${res.inserted.toLocaleString()} new unique record(s)` +
          (res.skipped > 0
            ? `; dropped ${res.skipped.toLocaleString()} overlong line(s).`
            : ".")
      );
      setFile(null);
      if (inputRef.current) inputRef.current.value = "";
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
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

      {busy && (
        <div style={{ marginTop: 12 }}>
          <div style={{ background: "var(--panel-2)", borderRadius: 6, overflow: "hidden" }}>
            <div
              style={{
                width: `${progress * 100}%`,
                height: 8,
                background: "var(--accent)",
                transition: "width 0.2s",
              }}
            />
          </div>
          <div className="muted" style={{ marginTop: 4 }}>
            {(progress * 100).toFixed(1)}%
          </div>
        </div>
      )}

      {result && <div className="ok" style={{ marginTop: 12 }}>{result}</div>}
      {error && <div className="error" style={{ marginTop: 12 }}>{error}</div>}
    </div>
  );
}
