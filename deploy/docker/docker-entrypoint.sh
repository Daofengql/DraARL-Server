#!/bin/sh
set -eu

config_path="${DRAARL_CONFIG_PATH:-/var/lib/draarl/config.yaml}"
template_path="/etc/draarl/config.yaml.template"

generate_hex() {
    od -An -N "$1" -tx1 /dev/urandom | tr -d ' \n'
}

require_value() {
    eval "value=\${$1:-}"
    if [ -z "$value" ]; then
        echo "DraARL container: $1 is required" >&2
        exit 1
    fi
}

if [ ! -s "$config_path" ]; then
    require_value DRAARL_PUBLIC_URL
    require_value DRAARL_MYSQL_PASSWORD
    require_value DRAARL_REDIS_PASSWORD
    require_value DRAARL_MINIO_ROOT_USER
    require_value DRAARL_MINIO_ROOT_PASSWORD
    require_value DRAARL_MINIO_ENDPOINT

    if [ -z "${DRAARL_JWT_SECRET:-}" ]; then
        DRAARL_JWT_SECRET="$(generate_hex 32)"
    fi
    if [ -z "${DRAARL_AES_KEY:-}" ]; then
        DRAARL_AES_KEY="$(generate_hex 16)"
    fi
    DRAARL_MINIO_USE_SSL="${DRAARL_MINIO_USE_SSL:-false}"
    export DRAARL_PUBLIC_URL DRAARL_MYSQL_PASSWORD DRAARL_REDIS_PASSWORD
    export DRAARL_MINIO_ROOT_USER DRAARL_MINIO_ROOT_PASSWORD
    export DRAARL_MINIO_ENDPOINT DRAARL_MINIO_USE_SSL
    export DRAARL_JWT_SECRET DRAARL_AES_KEY

    umask 077
    mkdir -p "$(dirname "$config_path")"
    temporary="${config_path}.tmp"
    envsubst < "$template_path" > "$temporary"
    mv "$temporary" "$config_path"
    echo "DraARL container: generated persistent config at $config_path"
fi

if [ "${DRAARL_AUTO_MIGRATE:-true}" = "true" ]; then
    set -- -auto-migrate "$@"
fi

exec /usr/local/bin/draarl -c "$config_path" "$@"
