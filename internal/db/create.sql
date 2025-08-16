-- for reference
CREATE TABLE IF NOT EXISTS dhammapada_quotes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  verse_number INTEGER UNIQUE NOT NULL,
  quote TEXT NOT NULL,
  image_path TEXT NOT NULL,
  posted_at TIMESTAMP NULL
);

