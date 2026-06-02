CREATE TABLE dogs (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL,
    accent_color    TEXT NOT NULL DEFAULT '#A8D8B9',
    photo_path      TEXT,
    sort_order      INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at      DATETIME
);

CREATE TABLE feedings (
    id              INTEGER PRIMARY KEY,
    dog_id          INTEGER NOT NULL REFERENCES dogs(id) ON DELETE RESTRICT,
    ts_utc          DATETIME NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('standard','nonstandard')) DEFAULT 'standard',
    score           TEXT NOT NULL CHECK (score IN ('full','partial','none')),
    specifics       TEXT,
    source          TEXT NOT NULL CHECK (source IN ('button','web')),
    deleted_at      DATETIME,
    edited_at       DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_feedings_dog_ts ON feedings(dog_id, ts_utc) WHERE deleted_at IS NULL;
CREATE INDEX idx_feedings_ts ON feedings(ts_utc) WHERE deleted_at IS NULL;

CREATE TABLE snacks (
    id              INTEGER PRIMARY KEY,
    dog_id          INTEGER NOT NULL REFERENCES dogs(id) ON DELETE RESTRICT,
    ts_utc          DATETIME NOT NULL,
    specifics       TEXT,
    source          TEXT NOT NULL CHECK (source IN ('button','web')),
    deleted_at      DATETIME,
    edited_at       DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_snacks_dog_ts ON snacks(dog_id, ts_utc) WHERE deleted_at IS NULL;
CREATE INDEX idx_snacks_ts ON snacks(ts_utc) WHERE deleted_at IS NULL;

CREATE TABLE illness_events (
    id              INTEGER PRIMARY KEY,
    dog_id          INTEGER NOT NULL REFERENCES dogs(id) ON DELETE RESTRICT,
    start_date      DATE NOT NULL,
    end_date        DATE,
    notes           TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_illness_dog ON illness_events(dog_id, start_date);

CREATE TABLE stress_events (
    id              INTEGER PRIMARY KEY,
    dog_id          INTEGER REFERENCES dogs(id) ON DELETE RESTRICT,
    start_date      DATE NOT NULL,
    end_date        DATE,
    kind            TEXT,
    notes           TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_stress_start ON stress_events(start_date);

CREATE TABLE device_state (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    locked_until_utc    DATETIME,
    last_lock_reason    TEXT,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO device_state (id) VALUES (1);
