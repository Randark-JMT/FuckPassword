import { useState } from "react";
import { submitJob } from "../api";

export default function QueryView({ onSubmitted }: { onSubmitted: (id: string) => void }) {
  const [pattern, setPattern] = useState("");
  const [isRegex, setIsRegex] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit() {
    if (!pattern.trim()) return;
    setBusy(true);
    setError(null);
    try {
      const res = await submitJob(pattern, isRegex);
      onSubmitted(res.task_id);
      setPattern("");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="panel">
      <label htmlFor="pattern">Query expression</label>
      <textarea
        id="pattern"
        value={pattern}
        onChange={(e) => setPattern(e.target.value)}
        placeholder={
          isRegex
            ? "regular expression, e.g. ^https?://.*admin"
            : "substring, case-insensitive, e.g. admin@test.com"
        }
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) handleSubmit();
        }}
      />

      <div className="row" style={{ marginTop: 12 }}>
        <label className="switch">
          <input
            type="checkbox"
            checked={isRegex}
            onChange={(e) => setIsRegex(e.target.checked)}
          />
          <span className="track" />
          <span>Regex {isRegex ? "ON" : "OFF"}</span>
        </label>
        <span className="muted">
          {isRegex
            ? "match-anywhere, case-sensitive (use (?i) to relax)"
            : "case-insensitive substring match"}
        </span>
        <div className="grow" />
        <button className="primary" disabled={!pattern.trim() || busy} onClick={handleSubmit}>
          {busy ? "Submitting…" : "Search"}
        </button>
      </div>

      {error && <div className="error" style={{ marginTop: 12 }}>{error}</div>}
      <p className="muted" style={{ marginTop: 12 }}>
        Jobs run one at a time in arrival order. You will be taken to the Status Board, where
        you can watch progress and cancel.
      </p>
    </div>
  );
}
