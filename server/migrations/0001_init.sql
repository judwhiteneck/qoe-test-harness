-- LLD/L4S validation tool — initial schema
-- Postgres. Additive migration; rollback = drop these tables.

create extension if not exists "pgcrypto";  -- gen_random_uuid()

create table tester (
  id            uuid primary key default gen_random_uuid(),
  access_code   text unique not null,
  label         text,
  created_at    timestamptz not null default now()
);

create table run (
  id                uuid primary key default gen_random_uuid(),
  tester_id         uuid not null references tester(id),
  started_at        timestamptz not null,
  app_version       text,
  os_version        text,
  device_model      text,
  conn_type         text,          -- wifi | wired
  wifi_band         text,
  wifi_rssi         int,
  modem_model       text,
  modem_fw          text,
  cmts_id           text,
  isp               text,
  asn               int,
  service_tier_down int,           -- Mbps
  service_tier_up   int,           -- Mbps
  geo_coarse        text,          -- city/ZIP level, consented
  public_ip         inet,
  consent_location  boolean not null default false,
  verdict           text,          -- pass | fail | inconclusive
  base_rtt_down_us  int,
  base_rtt_up_us    int
);

create index run_tester_idx on run (tester_id);
create index run_started_idx on run (started_at);

create table phase_result (
  id               uuid primary key default gen_random_uuid(),
  run_id           uuid not null references run(id) on delete cascade,
  phase            text not null,        -- baseline | down_loaded | up_loaded | no_harm
  flow_type        text not null,        -- ll | classic
  direction        text not null,        -- down | up
  throughput_bps   bigint,
  marking_survival numeric,              -- 0..1
  ce_mark_rate     numeric,              -- 0..1
  hist_edges_ms    numeric[] not null,   -- fixed edge array
  hist_counts      bigint[] not null,    -- mergeable bin counts on working_latency_delta
  sample_count     bigint not null
);

create index phase_run_idx on phase_result (run_id);

-- Raw samples kept for re-analysis (~MB scale at <100 testers). Retention 180d.
create table raw_sample (
  run_id     uuid not null references run(id) on delete cascade,
  phase      text not null,
  flow_type  text,
  direction  text,
  seq        bigint,
  delta_us   int,                        -- working_latency_delta
  tos_sent   smallint,
  tos_echo   smallint
);

create index raw_sample_run_idx on raw_sample (run_id);
