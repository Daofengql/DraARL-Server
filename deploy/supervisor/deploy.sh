#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 1 || ! $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo 'Usage: draarl-deploy vMAJOR.MINOR.PATCH[-prerelease]' >&2
  exit 2
fi
version=$1
source "${DRAARL_DEPLOY_CONFIG:-/etc/draarl-deploy.conf}"

archive=DraARL-linux-amd64.tar.gz
binary=DraARL-linux-amd64
release_url="${RELEASE_BASE_URL:-https://github.com/Daofengql/DraARL-Server/releases/download}/$version"
state_dir=${STATE_DIR:-/var/lib/draarl-deploy}
install -d -m 0700 "$state_dir"
exec 9>"$state_dir/deploy.lock"
flock -w 300 9

supervisor() {
  timeout 60 "$SUPERVISORCTL" -c "$SUPERVISOR_CONFIG" "$@" "$SUPERVISOR_PROGRAM"
}

ready() {
  local expected=$1
  curl --fail --silent --max-time 3 "$HEALTH_URL" >/dev/null &&
    curl --fail --silent --max-time 3 "$VERSION_URL" |
      grep -Fq '"version":"'"$expected"'"'
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
changed=0
finish() {
  local result=$?
  trap - EXIT HUP INT TERM
  if ((changed)); then
    echo "Deployment failed; restoring $previous_version" >&2
    if cp -p "$state_dir/server.previous" "$stage/server.rollback" &&
      mv -f "$stage/server.rollback" "$APP_DIR/server" &&
      supervisor restart && wait_ready "$previous_version"; then
      echo "Restored $previous_version" >&2
    else
      echo 'Rollback needs operator attention; previous binary is in the deployment state directory' >&2
    fi
  fi
  rm -rf -- "$stage"
  exit "$result"
}
trap finish EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Downloading $version"
for asset in "$archive" "$archive.sha256"; do
  curl --fail --silent --show-error --location --connect-timeout 10 \
    --max-time 180 --retry 2 --retry-max-time 240 \
    "$release_url/$asset" -o "$stage/$asset"
done
read -r expected_hash _ < "$stage/$archive.sha256"
[[ $expected_hash =~ ^[[:xdigit:]]{64}$ ]]
printf '%s  %s\n' "$expected_hash" "$stage/$archive" | sha256sum --check --status
tar -xOf "$stage/$archive" "./$binary" > "$stage/server.next"
chmod --reference="$APP_DIR/server" "$stage/server.next"
chown --reference="$APP_DIR/server" "$stage/server.next"
candidate_version=$("$stage/server.next" -v)
[[ $candidate_version == "DraARL version $version (build time: "* ]]

if cmp -s "$APP_DIR/server" "$stage/server.next" && ready "$version"; then
  echo "Already running $version"
  exit 0
fi

previous_version=$("$APP_DIR/server" -v | sed -n 's/^DraARL version \([^ ]*\) .*/\1/p')
[[ -n $previous_version ]]
cp -p "$APP_DIR/server" "$state_dir/server.previous.next"
mv -f "$state_dir/server.previous.next" "$state_dir/server.previous"
changed=1
mv -f "$stage/server.next" "$APP_DIR/server"
supervisor restart
if ! wait_ready "$version"; then
  echo "Readiness/version check failed for $version" >&2
  exit 1
fi
printf '%s\n' "$version" > "$state_dir/current-version"
changed=0
echo "Deployed $version; readiness and running version verified"
