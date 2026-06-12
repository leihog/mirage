#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
cd "$ROOT"

docker compose -f tests/docker-compose.yml up -d --build

printf 'Mirage is running at http://localhost:18025\n'
printf 'Mirage SMTP is listening at localhost:11025\n'
