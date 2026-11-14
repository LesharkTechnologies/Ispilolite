# Database Backup and Disaster Recovery

## Backup policy

- Run `scripts/backup-postgres.sh` daily at 02:30 UTC via `ispilolite-backup.timer`.
- Keep 30 daily backups locally, weekly copies for 12 weeks, and monthly copies for 12 months.
- Store an encrypted copy in a separate region/account (rclone remote or custom command).
- Enable PostgreSQL WAL archiving for point-in-time recovery between full backups.
- Alert when the last successful backup is older than 26 hours
  (`IspiloliteBackupStale` in `docker/prometheus/alerts.yml`).

The script creates a compressed custom-format dump, validates it with
`pg_restore --list`, writes a SHA-256 checksum, prunes old local files, and
publishes `ispilo_backup_*` metrics for the textfile collector.

## One-time setup (Ubuntu VPS)

```bash
# 1. Backup role with read access only.
sudo -u postgres psql <<'SQL'
CREATE ROLE ispilo_backup LOGIN PASSWORD '<STRONG_BACKUP_PASSWORD>';
GRANT pg_read_all_data TO ispilo_backup;
SQL

# 2. Directories.
sudo install -d -o ispilo -g ispilo -m 0700 /var/backups/ispilolite
sudo install -d -o ispilo -g ispilo -m 0755 /var/lib/node_exporter/textfile
sudo install -d -o root -g root -m 0750 /etc/ispilolite

# 3. Secrets and schedule configuration.
sudo cp deploy/systemd/ispilolite-backup.env.example /etc/ispilolite/backup.env
sudo chmod 600 /etc/ispilolite/backup.env
sudo editor /etc/ispilolite/backup.env

# 4. Offsite tooling (pick one).
sudo apt install -y rclone          # S3/B2/GCS/SFTP remotes
# or leave BACKUP_OFFSITE_REMOTE empty and use BACKUP_OFFSITE_CMD.
# The timer fails if both are unset.

# 5. Install the timer.
sudo cp deploy/systemd/ispilolite-backup.service /etc/systemd/system/
sudo cp deploy/systemd/ispilolite-backup.timer   /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ispilolite-backup.timer

# 6. Prove it works now, not at 02:30.
sudo systemctl start ispilolite-backup.service
sudo journalctl -u ispilolite-backup.service -n 50 --no-pager
sudo systemctl list-timers ispilolite-backup.timer
```

For an encrypted offsite copy, set `BACKUP_GPG_RECIPIENT` to an operator key
that is **not** stored on the database host. The local dump stays unencrypted
under `/var/backups/ispilolite` (mode 0700) so restores are fast; the uploaded
`.gpg` file and its checksum are encrypted.

Configure the remote's object lifecycle so the offsite retention matches the
policy above (for example, S3 lifecycle rules or a B2 bucket lifecycle).

## Restore procedure

1. Provision an isolated PostgreSQL instance at the same or newer compatible
   version. Never restore over the failed primary during the first attempt.
2. Restore the latest backup:

   ```bash
   export DATABASE_URL='postgres://ispilo_app:<password>@127.0.0.1:5432/ispilolite_restore'
   ./scripts/restore-postgres.sh /var/backups/ispilolite/ispilolite_<stamp>.dump
   ```

   The script verifies the `.sha256` file, validates the archive, asks for
   confirmation, and restores in a single transaction. Pass `--yes` for
   automation only after the isolated instance is confirmed empty.
3. Apply archived WAL to the selected recovery timestamp when point-in-time
   recovery is required.
4. Run `go run ./cmd/migrate` against the restored instance, then run smoke
   tests and row-count checks.
5. Pause application writes, capture a final backup, and switch the database
   secret/DNS endpoint.
6. Monitor error rate, PostgreSQL locks and replica lag, queue depth, and the
   search fallback rate before resuming normal traffic.

For an encrypted dump, pass the `.gpg` file directly:
`./scripts/restore-postgres.sh .../ispilolite_<stamp>.dump.gpg`.

## Verification and drills

- `systemctl list-timers ispilolite-backup.timer` shows the next run.
- `ispilo_backup_last_success_timestamp_seconds` must advance daily; the
  `IspiloliteBackupStale` alert fires after 26 hours.
- Test a restore quarterly and record duration, checksum, row counts, and
  sign-off. A backup that has never been restored is not a backup.

## Targets

- Recovery point objective: 15 minutes with WAL archiving, 24 hours without it.
- Recovery time objective: 60 minutes for the primary database.
- Never overwrite the failed primary during the first restore attempt. Preserve
  it for investigation and possible incremental recovery.

## Windows development

`scripts/backup-postgres.ps1` and `scripts/restore-postgres.ps1` remain for
local Windows work. Production uses the Bash scripts and systemd timer above.
