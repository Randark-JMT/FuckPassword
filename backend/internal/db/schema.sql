CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS records (
    id   bigserial PRIMARY KEY,
    text text NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS records_text_uq ON records (text);
CREATE INDEX IF NOT EXISTS records_text_trgm     ON records USING gin (text gin_trgm_ops);

DO $$ BEGIN
    CREATE TYPE job_status AS ENUM ('queued','running','completed','failed','cancelled');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS query_jobs (
    id          uuid PRIMARY KEY,
    pattern     text NOT NULL,
    is_regex    boolean NOT NULL,
    status      job_status NOT NULL DEFAULT 'queued',
    match_count integer,
    error       text,
    pid         integer,
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz,
    position    bigserial NOT NULL
);

CREATE INDEX IF NOT EXISTS query_jobs_status_pos_idx
    ON query_jobs (status, position);

-- A row that flips from 'queued'/'running' to terminal states via this trigger
-- always stamps finished_at, so TTL reaping can find expired terminal jobs.
CREATE OR REPLACE FUNCTION stamp_finished() RETURNS trigger AS $$
BEGIN
    IF NEW.status IN ('completed','failed','cancelled') AND OLD.status NOT IN ('completed','failed','cancelled') THEN
        NEW.finished_at := now();
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS query_jobs_stamp_finished ON query_jobs;
CREATE TRIGGER query_jobs_stamp_finished BEFORE UPDATE ON query_jobs
    FOR EACH ROW EXECUTE FUNCTION stamp_finished();
