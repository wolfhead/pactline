#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
compose_file=${PACTLINE_COMPOSE_FILE:-"$repository_dir/compose.production.yaml"}
environment_file=${PACTLINE_ENV_FILE:-"$script_dir/.env"}
backup_dir=${PACTLINE_BACKUP_DIR:-"$script_dir/backups"}

if [ ! -f "$environment_file" ]; then
	echo "production environment file not found: $environment_file" >&2
	exit 1
fi

mkdir -p "$backup_dir"
chmod 700 "$backup_dir"
umask 077

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
backup_file="$backup_dir/pactline-$timestamp.dump"
partial_file="$backup_file.partial"

cleanup() {
	if [ -f "$partial_file" ]; then
		rm -f -- "$partial_file"
	fi
}
trap cleanup EXIT INT TERM

docker compose \
	--env-file "$environment_file" \
	-f "$compose_file" \
	exec -T postgres \
	sh -c 'pg_dump --format=custom --no-owner --no-acl --username="$POSTGRES_USER" "$POSTGRES_DB"' \
	>"$partial_file"

mv "$partial_file" "$backup_file"
trap - EXIT INT TERM
echo "$backup_file"
