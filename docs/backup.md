# Backup

One SQLite file: `DATA_DIR/pocket-health.db` (WAL mode). Take a consistent copy with `pocket-health backup` (writes `DATA_DIR/backups/pocket-health-<time>.db` with `VACUUM INTO`) or Settings → Backup & data → Download database (admin). Back up that copy, not the live file. `DATA_DIR/files/imports` holds in-flight Apple Health uploads only and can be excluded; `DATA_DIR/core.token*` is the Pocket Core registration.

Restore: stop the app, replace `pocket-health.db` (remove `-wal`/`-shm` files), start.

Every person can also export their own data as JSON (`/api/v1/export.json`, re-importable) or the daily numbers as CSV.
