# Database operations

Proxyma stores commands, interaction history, and durable interaction state in
SQLite at `var/proxyma/database.db` by default. Override the path with
`database_path`, `--database-path`, or `PROXYMA_DATABASE_PATH`.

The database is created automatically on startup. Embedded numbered migrations
create and upgrade `schema_migrations`, `commands`, `interactions`, and their
FTS5 search tables. Do not edit those tables manually.

## Online backup

Set `PROXYMA_DATABASE_PATH` to the configured path, or substitute the path
directly:

```bash
sqlite3 "$PROXYMA_DATABASE_PATH" ".backup '$BACKUP_PATH'"
sqlite3 "$BACKUP_PATH" "PRAGMA integrity_check;"
```

The integrity check should print `ok`. Keep dated backups with a retention
policy, take a backup before upgrades, and periodically test restoring one.
Backups can contain command output and interaction messages, so protect them
with restricted file permissions and secure storage. Never put credentials in
shell history or committed configuration.

## Stopped-server backup

1. Stop Proxyma.
2. Copy `database.db` to protected backup storage.
3. If present, copy the matching `database.db-wal` and `database.db-shm` files
   together with it, or use the online backup method instead.
4. Protect and verify the backup.
5. Restart Proxyma.

## Restore

Stop Proxyma, preserve the current database, place the verified backup at the
configured database path, and restart Proxyma. SQLite migrations will verify
the restored schema. Keep the old database until the restored server has been
validated.

## Reset from scratch

There is deliberately no destructive `proxymactl` reset command. To recreate
an empty database safely:

1. Stop Proxyma.
2. Back up the current database.
3. Move it aside instead of deleting it immediately:

   ```bash
   mv var/proxyma/database.db \
      var/proxyma/database.db.before-reset-YYYY-MM-DD
   ```

4. Remove stale WAL sidecars if they remain:

   ```bash
   rm -f var/proxyma/database.db-wal
   rm -f var/proxyma/database.db-shm
   ```

5. Restart Proxyma.

Proxyma creates a new database and reapplies migrations. JSON configuration
resources—including users, agents, targets, policies, and interaction-instance
provider configuration—are not changed. Never move or remove the database while
Proxyma is running.
