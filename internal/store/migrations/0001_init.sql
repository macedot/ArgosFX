-- 0001_init.sql
CREATE TABLE IF NOT EXISTS providers (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    type            TEXT NOT NULL,
    config_json     TEXT NOT NULL DEFAULT '{}',
    priority        INTEGER NOT NULL DEFAULT 0,
    calls_per_day   INTEGER,
    schedule_cron   TEXT,
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS readings (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id     INTEGER NOT NULL REFERENCES providers(id),
    base            TEXT NOT NULL,
    quote           TEXT NOT NULL,
    rate            REAL NOT NULL,
    fetched_at      TEXT NOT NULL,
    provider_ts     TEXT
);

CREATE INDEX IF NOT EXISTS ix_readings_lookup
    ON readings(quote, fetched_at DESC);

CREATE INDEX IF NOT EXISTS ix_readings_provider
    ON readings(provider_id, fetched_at DESC);

CREATE TABLE IF NOT EXISTS usage (
    provider_id     INTEGER NOT NULL,
    day             TEXT NOT NULL,
    count           INTEGER NOT NULL,
    PRIMARY KEY (provider_id, day)
);
