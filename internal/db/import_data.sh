#!/usr/bin/env bash

-- import from csv
sqlite3 ../../data/dhammapada.sqlite <<'SQL'
.mode csv
.import ../../data/texts.csv texts

-- sanity checks
.headers on
SELECT COUNT(*) AS texts_rows FROM texts;
SELECT id,label FROM texts ORDER BY id LIMIT 5;
SQL
