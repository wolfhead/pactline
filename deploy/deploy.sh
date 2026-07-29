#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
compose_file=${PACTLINE_COMPOSE_FILE:-"$repository_dir/compose.production.yaml"}
environment_file=${PACTLINE_ENV_FILE:-"$script_dir/.env"}

if [ ! -f "$environment_file" ]; then
	echo "production environment file not found: $environment_file" >&2
	exit 1
fi

compose() {
	docker compose --env-file "$environment_file" -f "$compose_file" "$@"
}

compose config --quiet

if compose ps --status running --services | grep -qx postgres; then
	echo "Creating a PostgreSQL backup before deployment"
	PACTLINE_COMPOSE_FILE="$compose_file" \
		PACTLINE_ENV_FILE="$environment_file" \
		"$script_dir/backup.sh"
fi

echo "Pulling immutable Pactline images"
compose pull

echo "Starting Pactline"
compose up -d --wait --remove-orphans

echo "Verifying the gateway and API from inside the deployment network"
compose exec -T web wget -qO- http://127.0.0.1:8080/healthz >/dev/null
compose exec -T api wget -qO- http://127.0.0.1:8080/readyz >/dev/null

compose ps
