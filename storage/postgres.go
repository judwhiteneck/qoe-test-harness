package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/judwhiteneck/qoe-test-harness/core/report"
)

// Postgres is a Store backed by Postgres. It uses only database/sql; the caller
// supplies a *sql.DB opened with whatever driver they registered (e.g. pgx or
// lib/pq), so this package adds no third-party dependency. Apply schema.sql once
// before use. Indexed columns are denormalised out of the JSON payload for
// filtering; the payload is the source of truth on read.
type Postgres struct{ db *sql.DB }

// NewPostgres wraps an already-open *sql.DB.
func NewPostgres(db *sql.DB) *Postgres { return &Postgres{db: db} }

// Save upserts a run by run_id.
func (p *Postgres) Save(ctx context.Context, r report.RunReport) error {
	payload, err := json.Marshal(r)
	if err != nil {
		return err
	}
	const q = `
INSERT INTO runs (run_id, started_at_ms, isp, region, device_class, verdict, provisioned_down_bps, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (run_id) DO UPDATE SET
  started_at_ms = EXCLUDED.started_at_ms,
  isp = EXCLUDED.isp,
  region = EXCLUDED.region,
  device_class = EXCLUDED.device_class,
  verdict = EXCLUDED.verdict,
  provisioned_down_bps = EXCLUDED.provisioned_down_bps,
  payload = EXCLUDED.payload`
	_, err = p.db.ExecContext(ctx, q,
		r.Meta.RunID,
		r.Meta.StartedAtUnixMs,
		r.Meta.ISP,
		r.Meta.Region,
		r.Meta.DeviceClass,
		string(r.Result.Verdict),
		int64(r.Meta.ProvisionedDownBps),
		payload,
	)
	return err
}

// List returns matching runs newest-first, bounded by the filter's limit.
func (p *Postgres) List(ctx context.Context, f Filter) ([]report.RunReport, error) {
	var where []string
	var args []any
	add := func(col, val string) {
		if val != "" {
			args = append(args, val)
			where = append(where, col+" = $"+strconv.Itoa(len(args)))
		}
	}
	add("isp", f.ISP)
	add("region", f.Region)
	add("device_class", f.DeviceClass)
	add("verdict", f.Verdict)
	if f.SinceUnixMs != 0 {
		args = append(args, f.SinceUnixMs)
		where = append(where, "started_at_ms >= $"+strconv.Itoa(len(args)))
	}

	q := "SELECT payload FROM runs"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, f.limit())
	q += " ORDER BY started_at_ms DESC LIMIT $" + strconv.Itoa(len(args))

	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []report.RunReport
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var r report.RunReport
		if err := json.Unmarshal(payload, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

var _ Store = (*Postgres)(nil)
