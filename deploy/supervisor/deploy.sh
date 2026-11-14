#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $# != 1 || ! $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo 'Usage: draarl-deploy vMAJOR.MINOR.PATCH[-prerelease]' >&2
  exit 2
fi
version=$1
source "${DRAARL_DEPLOY_CONFIG:-/etc/draarl-deploy.conf}"

archive=DraARL-linux-amd64.tar.gz
state_dir=${STATE_DIR:-/var/lib/draarl-deploy}
install -d -m 0700 "$state_dir" "$state_dir/backups"
exec 9>"$state_dir/deploy.lock"
flock -w 300 9
if [[ -e $state_dir/deployment-failed ]]; then
  echo "Review the previous database migration failure in $state_dir before deploying again" >&2
  exit 1
fi

supervisor() {
  timeout 60 "$SUPERVISORCTL" -c "$SUPERVISOR_CONFIG" "$@" "$SUPERVISOR_PROGRAM"
}

ready() {
  local expected=$1
  curl --fail --silent --max-time 3 "$HEALTH_URL" >/dev/null &&
    curl --fail --silent --max-time 3 "$VERSION_URL" |
      grep -F '"version":"'"$expected"'"' >/dev/null
}

wait_ready() {
  local attempt
  for ((attempt=0; attempt<${HEALTH_ATTEMPTS:-30}; attempt++)); do
    if ready "$1"; then return 0; fi
    sleep "${HEALTH_INTERVAL:-2}"
  done
  return 1
}

stage=$(mktemp -d "$APP_DIR/.draarl-deploy.XXXXXX")
stopped=0
migration_started=0
backup_partial=''
finish() {
  local result=$?
  trap - EXIT HUP INT TERM
  if ((stopped)); then
    if ((migration_started)); then
      supervisor stop || true
      printf 'version=%s\nbackup=%s\nlog=%s\n' "$version" "$backup" "$migration_log" > "$state_dir/deployment-failed"
      echo "Deployment stopped after database migration began. Backup: $backup; log: $migration_log" >&2
      echo 'Service remains stopped for recovery; database and older program versions must be reviewed together' >&2
    elif supervisor start && wait_ready "$previous_version"; then
      echo "Restored service $previous_version; database migration was not started" >&2
    else
      echo 'Restoring the previous service needs operator attention' >&2
    fi
  fi
  if [[ -n $backup_partial ]]; then rm -f -- "$backup_partial"; fi
  rm -rf -- "$stage"
  exit "$result"
}
trap finish EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# The runner downloads the Release; production receives a checksum line and the
# archive over authenticated SSH, without a second GitHub download.
read -r expected_hash _
[[ $expected_hash =~ ^[[:xdigit:]]{64}$ ]]
cat > "$stage/$archive"
printf '%s  %s\n' "$expected_hash" "$stage/$archive" | sha256sum --check --status
tar -xOf "$stage/$archive" ./DraARL-linux-amd64 > "$stage/server.next"
chmod --reference="$APP_DIR/server" "$stage/server.next"
chown --reference="$APP_DIR/server" "$stage/server.next"
candidate_version=$("$stage/server.next" -v)
[[ $candidate_version == "DraARL version $version (build time: "* ]]

if cmp -s "$APP_DIR/server" "$stage/server.next" && ready "$version"; then
  echo "Already running $version"
  exit 0
fi
"$stage/server.next" -h 2>&1 | grep -- '-migrate-only' >/dev/null

previous_version=$("$APP_DIR/server" -v | sed -n 's/^DraARL version \([^ ]*\) .*/\1/p')
[[ -n $previous_version ]]
cp -p "$APP_DIR/server" "$state_dir/server.previous.next"
mv -f "$state_dir/server.previous.next" "$state_dir/server.previous"
backup="$state_dir/backups/$(date -u +%Y%m%dT%H%M%SZ)-$version.sql.gz"
backup_partial="$backup.partial"
migration_log="$state_dir/migration-$version.log"
stopped=1
supervisor stop
echo 'Backing up the stopped application database'
timeout "${BACKUP_TIMEOUT:-300}" "$MYSQLDUMP" --defaults-extra-file="$MYSQL_DEFAULTS_FILE" \
  --single-transaction --no-tablespaces --routines --events --triggers --hex-blob \
  --databases "$DATABASE_NAME" | gzip > "$backup_partial"
gzip -t "$backup_partial"
mv -f "$backup_partial" "$backup"
backup_partial=''
echo "Database backup saved: $backup"
migration_started=1
if ! timeout "${MIGRATION_TIMEOUT:-300}" "$stage/server.next" -c "$APP_CONFIG" -migrate-only > "$migration_log" 2>&1; then
  echo "Database migration failed; see $migration_log" >&2
  exit 1
fi
mv -f "$stage/server.next" "$APP_DIR/server"
supervisor start
if ! wait_ready "$version"; then
  echo "Readiness/version check failed for $version" >&2
  exit 1
fi
printf '%s\n' "$version" > "$state_dir/current-version"
stopped=0
# Keep the three most recent successful upgrade snapshots.
mapfile -t backups < <(printf '%s\n' "$state_dir"/backups/*.sql.gz | sort -r)
if ((${#backups[@]} > 3)); then rm -f -- "${backups[@]:3}"; fi
echo "Deployed $version; database migration, readiness and running version verified"
