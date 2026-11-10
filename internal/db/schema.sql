-- Esquema inicial de la plataforma Grupo Sur.
-- Ver instrucciones.md §6 para el detalle del modelo.

-- ── Auth / módulos ────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS users (
    id         INTEGER PRIMARY KEY,
    email      TEXT    NOT NULL UNIQUE,
    name       TEXT    NOT NULL DEFAULT '',
    is_admin   INTEGER NOT NULL DEFAULT 0,
    active     INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE IF NOT EXISTS modules (
    key  TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_modules (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    module_key TEXT    NOT NULL REFERENCES modules(key) ON DELETE CASCADE,
    PRIMARY KEY (user_id, module_key)
);

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT    PRIMARY KEY,            -- token opaco
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    expires_at TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

-- ── Dominio Visitas ───────────────────────────────────────────────────────
-- Grano: un procedimiento realizado (con fecha) dentro de una visita.

CREATE TABLE IF NOT EXISTS import_batches (
    id            INTEGER PRIMARY KEY,
    filename      TEXT    NOT NULL,
    imported_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    imported_by   TEXT    NOT NULL DEFAULT '',
    rows_total    INTEGER NOT NULL DEFAULT 0,   -- filas leídas del/los xlsx
    rows_inserted INTEGER NOT NULL DEFAULT 0,   -- nuevas (realizadas, no duplicadas)
    rows_skipped  INTEGER NOT NULL DEFAULT 0    -- planificadas + duplicadas
);

CREATE TABLE IF NOT EXISTS visit_procedures (
    id              INTEGER PRIMARY KEY,
    mrn             TEXT NOT NULL,
    study           TEXT NOT NULL,   -- Study Protocol Assignment
    visit_name      TEXT NOT NULL,   -- Visit Name
    procedure       TEXT NOT NULL,   -- Procedure
    coordinator     TEXT,            -- puede ser NULL
    procedure_date  TEXT NOT NULL,   -- ISO YYYY-MM-DD
    import_batch_id INTEGER REFERENCES import_batches(id) ON DELETE SET NULL,
    -- clave natural de deduplicación (verificada: 0 duplicados en el histórico)
    UNIQUE (mrn, study, visit_name, procedure, procedure_date)
);
CREATE INDEX IF NOT EXISTS idx_vp_study  ON visit_procedures(study);
CREATE INDEX IF NOT EXISTS idx_vp_visit  ON visit_procedures(visit_name);
CREATE INDEX IF NOT EXISTS idx_vp_proc   ON visit_procedures(procedure);
CREATE INDEX IF NOT EXISTS idx_vp_coord  ON visit_procedures(coordinator);
CREATE INDEX IF NOT EXISTS idx_vp_date   ON visit_procedures(procedure_date);
