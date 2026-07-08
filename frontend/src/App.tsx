import { useState } from "react";
import UploadView from "./components/UploadView";
import QueryView from "./components/QueryView";
import StatusBoard from "./components/StatusBoard";
import ResultsView from "./components/ResultsView";
import LogsView from "./components/LogsView";
import HistoryView from "./components/HistoryView";

type Tab = "upload" | "query" | "status" | "history" | "logs";

export default function App() {
  const [tab, setTab] = useState<Tab>("query");
  const [focusJob, setFocusJob] = useState<string | null>(null);

  return (
    <div className="app">
      <header className="topbar">
        <h1>FuckPassword</h1>
        <nav>
          <button className={tab === "query" ? "active" : ""} onClick={() => setTab("query")}>
            Query
          </button>
          <button className={tab === "status" ? "active" : ""} onClick={() => setTab("status")}>
            Status Board
          </button>
          <button className={tab === "history" ? "active" : ""} onClick={() => setTab("history")}>
            History
          </button>
          <button className={tab === "logs" ? "active" : ""} onClick={() => setTab("logs")}>
            Logs
          </button>
          <button className={tab === "upload" ? "active" : ""} onClick={() => setTab("upload")}>
            Upload
          </button>
        </nav>
      </header>

      <main>
        {tab === "upload" && <UploadView />}
        {tab === "query" && (
          <QueryView
            onSubmitted={(id) => {
              setFocusJob(id);
              setTab("status");
            }}
          />
        )}
        {tab === "status" && <StatusBoard onFocus={(id) => setFocusJob(id)} />}
        {tab === "history" && <HistoryView onFocus={(id) => setFocusJob(id)} />}
        {tab === "logs" && <LogsView />}
      </main>

      {focusJob && (
        <section className="results-panel">
          <div className="results-head">
            <h2>Result</h2>
            <button className="link" onClick={() => setFocusJob(null)}>
              close
            </button>
          </div>
          <ResultsView jobId={focusJob} />
        </section>
      )}
    </div>
  );
}
