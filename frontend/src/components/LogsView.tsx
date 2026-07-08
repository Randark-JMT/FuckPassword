import { useEffect, useMemo, useRef, useState } from "react";
import { getRecentLogs, type LogEvent } from "../api";

function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour12: false });
}

function formatFields(fields: Record<string, unknown> | undefined): string {
  if (!fields || Object.keys(fields).length === 0) return "";
  return Object.entries(fields)
    .map(([key, value]) => `${key}=${formatValue(value)}`)
    .join("  ");
}

function formatValue(value: unknown): string {
  if (typeof value === "number") {
    if (!Number.isFinite(value)) return String(value);
    if (Math.abs(value) >= 1000) return value.toLocaleString();
    return Number.isInteger(value) ? String(value) : value.toFixed(1);
  }
  if (typeof value === "boolean") return value ? "true" : "false";
  if (value === null || value === undefined) return "null";
  return String(value);
}

export default function LogsView() {
  const [events, setEvents] = useState<LogEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scrollerRef = useRef<HTMLDivElement>(null);

  function append(next: LogEvent | LogEvent[]) {
    const incoming = Array.isArray(next) ? next : [next];
    setEvents((prev) => {
      const seen = new Set(prev.map((ev) => ev.id));
      const merged = [...prev];
      for (const ev of incoming) {
        if (!seen.has(ev.id)) {
          seen.add(ev.id);
          merged.push(ev);
        }
      }
      return merged.sort((a, b) => a.id - b.id).slice(-500);
    });
  }

  useEffect(() => {
    let cancelled = false;

    getRecentLogs()
      .then((logs) => {
        if (!cancelled) append(logs);
      })
      .catch((e) => {
        if (!cancelled) setError((e as Error).message);
      });

    const source = new EventSource("/api/logs/stream");
    source.addEventListener("open", () => {
      if (!cancelled) {
        setConnected(true);
        setError(null);
      }
    });
    source.addEventListener("error", () => {
      if (!cancelled) {
        setConnected(false);
        setError("log stream disconnected");
      }
    });
    source.addEventListener("log", (event) => {
      if (cancelled) return;
      try {
        append(JSON.parse((event as MessageEvent).data) as LogEvent);
      } catch {
        setError("received malformed log event");
      }
    });

    return () => {
      cancelled = true;
      source.close();
    };
  }, []);

  useEffect(() => {
    const el = scrollerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [events.length]);

  const newestID = useMemo(() => (events.length > 0 ? events[events.length - 1].id : 0), [events]);

  return (
    <div className="panel logs-panel">
      <div className="row logs-toolbar">
        <h2 style={{ margin: 0 }}>Logs</h2>
        <span className={`pill ${connected ? "completed" : "queued"}`}>{connected ? "live" : "offline"}</span>
        <span className="muted">#{newestID}</span>
        <div className="grow" />
        <button onClick={() => setEvents([])}>clear</button>
      </div>

      {error && <div className="warn" style={{ marginBottom: 12 }}>{error}</div>}

      <div className="logs-list" ref={scrollerRef}>
        {events.length === 0 ? (
          <div className="muted log-empty">waiting for logs...</div>
        ) : (
          events.map((ev) => (
            <div className={`log-row ${ev.level}`} key={ev.id}>
              <span className="log-time">{formatTime(ev.time)}</span>
              <span className={`log-level ${ev.level}`}>{ev.level}</span>
              <span className="log-source">{ev.source}</span>
              <span className="log-message">{ev.message}</span>
              {ev.fields && <span className="log-fields">{formatFields(ev.fields)}</span>}
            </div>
          ))
        )}
      </div>
    </div>
  );
}

