-- convenience script to run this SQL exists at:
-- dhammapada/internal/db/create_database.sh

CREATE TABLE texts (
  id         INTEGER PRIMARY KEY,
  label      TEXT NOT NULL UNIQUE,
  text_body  TEXT NOT NULL,
  posted_at  TEXT NULL,
  x_post_id  TEXT NULL
);

CREATE TABLE images (
  id      INTEGER PRIMARY KEY,
  text_id INTEGER NOT NULL REFERENCES texts(id) ON DELETE CASCADE,
  path    TEXT NOT NULL,
  ord     INTEGER NOT NULL DEFAULT 1
);

