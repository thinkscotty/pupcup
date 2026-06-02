-- Add-in meal tags (build plan milestone 10.5): a configurable household
-- catalog of meal add-ins, and a many-to-many join to feedings.

-- Configurable household catalog of meal add-ins (shredded chicken, cheese, …).
CREATE TABLE feed_tags (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL,
    is_unspecified  INTEGER NOT NULL DEFAULT 0,  -- 1 = reserved "Other / name later" sentinel
    archived_at     DATETIME,                    -- soft-hide from pickers without losing history
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- Names are stored Title-Cased and matched case-insensitively (NOCASE), so
-- "cheese"/"Cheese" can't coexist; unique among live (non-archived) tags only.
CREATE UNIQUE INDEX idx_feed_tags_name ON feed_tags(name COLLATE NOCASE) WHERE archived_at IS NULL;

-- Many-to-many: a feeding carries zero or more add-in tags.
CREATE TABLE feeding_tags (
    feeding_id      INTEGER NOT NULL REFERENCES feedings(id) ON DELETE CASCADE,
    tag_id          INTEGER NOT NULL REFERENCES feed_tags(id) ON DELETE RESTRICT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (feeding_id, tag_id)
);
CREATE INDEX idx_feeding_tags_tag ON feeding_tags(tag_id);

-- Exactly one reserved sentinel (id = 1, inserted first): device "Other"
-- attaches this so the feeding is recorded immediately and surfaced on the
-- web "needs a name" queue. The starter catalog follows at ids 2–9; this list
-- mirrors seed.FeedTags() (internal/seed/seed_data.yaml) — keep the two in sync.
INSERT INTO feed_tags (id, name, is_unspecified) VALUES (1, 'Unspecified add-in', 1);
INSERT INTO feed_tags (name) VALUES
    ('Shredded Chicken'),
    ('Cheese'),
    ('Parmesan'),
    ('Wet Food'),
    ('Freeze-Dried Liver'),
    ('Freeze-Dried Beef Patty'),
    ('Milk'),
    ('Rice');
