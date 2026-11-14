#!/usr/bin/env bash
set -euo pipefail

# The deployment key can pass only a version to this fixed, root-owned program.
exec /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin \
  /usr/bin/sudo -n /usr/local/sbin/draarl-deploy "${SSH_ORIGINAL_COMMAND:-}"
