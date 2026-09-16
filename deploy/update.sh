#!/bin/sh
set -eu
cd "$(dirname "$0")"
docker compose build --no-cache
docker compose up -d
