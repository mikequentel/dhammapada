-- convenience script to run this SQL exists at:
-- dhammapada/internal/db/create_database.sh

-- One row per text entity (either a single verse or a composite pair)
CREATE TABLE texts (
  id           INTEGER PRIMARY KEY,
  label        TEXT NOT NULL,     -- e.g., "58–59" or "151"
  text_body    TEXT NOT NULL,
  posted_at    TIMESTAMP NULL,    -- when this text entity was posted
  x_post_id    TEXT NULL          -- tweet ID if you want to track it
);

-- Map one text entity to 1..n verse numbers (n=1 for normal, n=2 for composites)
CREATE TABLE text_verses (
  text_id      INTEGER NOT NULL REFERENCES texts(id) ON DELETE CASCADE,
  verse_number INTEGER NOT NULL,
  PRIMARY KEY (text_id, verse_number)
);

-- Images can be attached to the text entity (shared), or optionally to specific verse numbers
CREATE TABLE images (
  id           INTEGER PRIMARY KEY,
  text_id      INTEGER NOT NULL REFERENCES texts(id) ON DELETE CASCADE,
  path         TEXT NOT NULL,
  ord          INTEGER NOT NULL DEFAULT 1
);

