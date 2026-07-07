export interface Job {
  id: string;
  pattern: string;
  is_regex: boolean;
  status: "queued" | "running" | "completed" | "failed" | "cancelled";
  match_count?: number | null;
  error?: string | null;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
  position: number;
}

async function jsonOrThrow<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const body = await res.json();
      if (body?.error) msg = body.error;
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  return res.json() as Promise<T>;
}

export async function uploadFile(file: File): Promise<{ inserted: number }> {
  const res = await fetch("/api/upload", { method: "POST", body: file });
  return jsonOrThrow(res);
}

export async function uploadBusy(): Promise<boolean> {
  // The server returns 409 when an upload is in progress; treat that as "busy".
  try {
    const res = await fetch("/api/upload", { method: "POST", body: new Blob([""]) });
    if (res.status === 409) return true;
  } catch {
    /* network error → assume not busy */
  }
  return false;
}

export async function submitJob(pattern: string, isRegex: boolean): Promise<{ task_id: string; status: string }> {
  const res = await fetch("/api/jobs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ pattern, is_regex: isRegex }),
  });
  return jsonOrThrow(res);
}

export async function getBoard(): Promise<{ running: Job | null; queued: Job[] }> {
  const res = await fetch("/api/jobs");
  return jsonOrThrow(res);
}

export async function getJob(id: string): Promise<Job> {
  const res = await fetch(`/api/jobs/${id}`);
  return jsonOrThrow(res);
}

export async function getResults(
  id: string,
  offset = 0,
  limit = 1000
): Promise<{ results: string[]; count: number; total: number; truncated: boolean }> {
  const res = await fetch(`/api/jobs/${id}/results?offset=${offset}&limit=${limit}`);
  return jsonOrThrow(res);
}

export function downloadURL(id: string): string {
  return `/api/jobs/${id}/download`;
}

export async function cancelJob(id: string): Promise<void> {
  const res = await fetch(`/api/jobs/${id}/cancel`, { method: "POST" });
  if (!res.ok) throw new Error("cancel failed");
}
