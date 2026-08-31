package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	sql *sql.DB
}

func Open(ctx context.Context, path string) (*DB, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	d := &DB{sql: db}
	if err := d.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func (d *DB) Close() error { return d.sql.Close() }

func (d *DB) migrate(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var applied int
		row := d.sql.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE name=?`, name)
		if err := row.Scan(&applied); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err := d.sql.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := d.sql.ExecContext(ctx, `INSERT INTO schema_migrations(name, applied_at) VALUES(?, ?)`, name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

type Provider struct {
	ID           int64
	Name         string
	Type         string
	Config       map[string]any
	Priority     int
	CallsPerDay  *int
	ScheduleCron string
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (d *DB) UpsertProvider(ctx context.Context, p Provider) (int64, error) {
	cfgJSON, _ := json.Marshal(p.Config)
	if cfgJSON == nil {
		cfgJSON = []byte("{}")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := d.sql.ExecContext(ctx, `
        INSERT INTO providers(name, type, config_json, priority, calls_per_day, schedule_cron, enabled, created_at, updated_at)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(name) DO UPDATE SET
            type=excluded.type,
            config_json=excluded.config_json,
            priority=excluded.priority,
            calls_per_day=excluded.calls_per_day,
            schedule_cron=excluded.schedule_cron,
            enabled=excluded.enabled,
            updated_at=excluded.updated_at
    `, p.Name, p.Type, string(cfgJSON), p.Priority, p.CallsPerDay, p.ScheduleCron, boolToInt(p.Enabled), now, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		if err := d.sql.QueryRowContext(ctx, `SELECT id FROM providers WHERE name=?`, p.Name).Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (d *DB) ListProviders(ctx context.Context) ([]Provider, error) {
	rows, err := d.sql.QueryContext(ctx, `
        SELECT id, name, type, config_json, priority, calls_per_day, schedule_cron, enabled, created_at, updated_at
        FROM providers ORDER BY priority DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		var p Provider
		var cfg, createdAt, updatedAt string
		var enabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &cfg, &p.Priority, &p.CallsPerDay, &p.ScheduleCron, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(cfg), &p.Config)
		p.Enabled = enabled != 0
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) GetProviderByName(ctx context.Context, name string) (Provider, error) {
	row := d.sql.QueryRowContext(ctx, `
        SELECT id, name, type, config_json, priority, calls_per_day, schedule_cron, enabled, created_at, updated_at
        FROM providers WHERE name=?`, name)
	var p Provider
	var cfg, createdAt, updatedAt string
	var enabled int
	if err := row.Scan(&p.ID, &p.Name, &p.Type, &cfg, &p.Priority, &p.CallsPerDay, &p.ScheduleCron, &enabled, &createdAt, &updatedAt); err != nil {
		return Provider{}, err
	}
	_ = json.Unmarshal([]byte(cfg), &p.Config)
	p.Enabled = enabled != 0
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return p, nil
}

type Reading struct {
	ID         int64
	ProviderID int64
	Base       string
	Quote      string
	Rate       float64
	FetchedAt  time.Time
	ProviderTS string
}

func (d *DB) InsertReading(ctx context.Context, r Reading) error {
	_, err := d.sql.ExecContext(ctx, `
        INSERT INTO readings(provider_id, base, quote, rate, fetched_at, provider_ts)
        VALUES(?, ?, ?, ?, ?, ?)
    `, r.ProviderID, r.Base, r.Quote, r.Rate, r.FetchedAt.UTC().Format(time.RFC3339), nullableStr(r.ProviderTS))
	return err
}

func (d *DB) LatestReadingsPerProvider(ctx context.Context, quote string, since time.Time) ([]Reading, []int64, error) {
	rows, err := d.sql.QueryContext(ctx, `
        SELECT r.id, r.provider_id, r.base, r.quote, r.rate, r.fetched_at, r.provider_ts
        FROM readings r
        JOIN (
            SELECT provider_id, MAX(fetched_at) AS max_at
            FROM readings
            WHERE quote = ? AND fetched_at >= ?
            GROUP BY provider_id
        ) latest
        ON r.provider_id = latest.provider_id AND r.fetched_at = latest.max_at
        WHERE r.quote = ?
    `, strings.ToUpper(quote), since.UTC().Format(time.RFC3339), strings.ToUpper(quote))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var readings []Reading
	var providerIDs []int64
	for rows.Next() {
		var r Reading
		var fetchedAt, providerTS sql.NullString
		if err := rows.Scan(&r.ID, &r.ProviderID, &r.Base, &r.Quote, &r.Rate, &fetchedAt, &providerTS); err != nil {
			return nil, nil, err
		}
		r.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt.String)
		if providerTS.Valid {
			r.ProviderTS = providerTS.String
		}
		readings = append(readings, r)
		providerIDs = append(providerIDs, r.ProviderID)
	}
	return readings, providerIDs, rows.Err()
}

func (d *DB) LatestReadingAt(ctx context.Context) (time.Time, bool, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT MAX(fetched_at) FROM readings`)
	var s sql.NullString
	if err := row.Scan(&s); err != nil {
		return time.Time{}, false, err
	}
	if !s.Valid {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}

func (d *DB) IncrementUsage(ctx context.Context, providerID int64, day time.Time) error {
	d_ := day.UTC().Format("2006-01-02")
	_, err := d.sql.ExecContext(ctx, `
        INSERT INTO usage(provider_id, day, count) VALUES(?, ?, 1)
        ON CONFLICT(provider_id, day) DO UPDATE SET count = count + 1
    `, providerID, d_)
	return err
}

func (d *DB) UsageToday(ctx context.Context, providerID int64, day time.Time) (int, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT count FROM usage WHERE provider_id=? AND day=?`,
		providerID, day.UTC().Format("2006-01-02"))
	var n int
	if err := row.Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}

type HistoryPoint struct {
	FetchedAt time.Time
	Rate      float64
}

func (d *DB) History(ctx context.Context, quote string, start, end time.Time, step time.Duration) ([]HistoryPoint, error) {
	rows, err := d.sql.QueryContext(ctx, `
        SELECT fetched_at, rate FROM readings
        WHERE quote = ? AND fetched_at >= ? AND fetched_at <= ?
        ORDER BY fetched_at ASC
    `, strings.ToUpper(quote), start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var raw []HistoryPoint
	for rows.Next() {
		var p HistoryPoint
		var s string
		if err := rows.Scan(&s, &p.Rate); err != nil {
			return nil, err
		}
		p.FetchedAt, _ = time.Parse(time.RFC3339, s)
		raw = append(raw, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if step <= 0 || len(raw) == 0 {
		return raw, nil
	}
	return downsample(raw, step), nil
}

func downsample(in []HistoryPoint, step time.Duration) []HistoryPoint {
	out := make([]HistoryPoint, 0, len(in))
	var bucketStart time.Time
	var sum, n float64
	flush := func() {
		if n > 0 {
			out = append(out, HistoryPoint{FetchedAt: bucketStart, Rate: sum / n})
		}
		sum, n = 0, 0
	}
	for _, p := range in {
		start := p.FetchedAt.Truncate(step)
		if !start.Equal(bucketStart) {
			flush()
			bucketStart = start
		}
		sum += p.Rate
		n++
	}
	flush()
	return out
}

func (d *DB) PruneOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM readings WHERE fetched_at < ?`,
		cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
