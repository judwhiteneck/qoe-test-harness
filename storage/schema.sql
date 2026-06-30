-- Schema for the engineer-view run store. Apply once on the VPS Postgres before
-- pointing the dashboard at it (the Postgres adapter does not auto-migrate).
-- The full RunReport (see core/report) lives in payload as JSONB; the columns
-- alongside it are denormalised purely for fast cohort filtering.

CREATE TABLE IF NOT EXISTS runs (
    run_id               text   PRIMARY KEY,
    started_at_ms        bigint NOT NULL,
    isp                  text,
    region               text,
    device_class         text,
    verdict              text   NOT NULL,
    provisioned_down_bps bigint,
    payload              jsonb  NOT NULL
);

CREATE INDEX IF NOT EXISTS runs_started_at_idx   ON runs (started_at_ms DESC);
CREATE INDEX IF NOT EXISTS runs_isp_idx          ON runs (isp);
CREATE INDEX IF NOT EXISTS runs_region_idx       ON runs (region);
CREATE INDEX IF NOT EXISTS runs_device_class_idx ON runs (device_class);
CREATE INDEX IF NOT EXISTS runs_verdict_idx      ON runs (verdict);
