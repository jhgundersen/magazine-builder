#!/bin/bash
# Root-owned forced SSH command. stdin is a Linux amd64 binary, never a shell script.
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
exec 9>/run/lock/magazine-builder-deploy.lock
flock -w 300 9
cd /root/website
stage=$(mktemp -d /root/website/magazine-builder/deploy.XXXXXX)
trap 'rm -rf "$stage"' EXIT
head -c 67108865 > "$stage/magazine-builder"
size=$(stat -c %s "$stage/magazine-builder")
(( size > 0 && size <= 67108864 )) || { echo 'Invalid binary size' >&2; exit 1; }
cp /root/website/magazine-builder/Dockerfile.runtime "$stage/Dockerfile"
docker build -t magazine-builder:candidate "$stage"
# Reject incompatible binaries before replacing the running service.
docker run --rm --network none magazine-builder:candidate version
compose=(docker compose -f docker-compose.yml -f magazine-builder/compose.deploy.yml)
previous=$(docker inspect --format '{{.Image}}' website-magazine-builder-1)
docker tag "$previous" magazine-builder:previous
docker tag magazine-builder:candidate magazine-builder:production
rollback() {
  echo 'Deployment failed; restoring previous image' >&2
  docker tag magazine-builder:previous magazine-builder:production
  "${compose[@]}" up -d --no-deps --no-build --force-recreate magazine-builder
}
if ! "${compose[@]}" up -d --no-deps --no-build --force-recreate magazine-builder; then
  rollback
  exit 1
fi
healthy=false
for attempt in $(seq 1 20); do
  if curl --fail --silent http://127.0.0.1:8080/static/app.js -o /dev/null; then
    healthy=true
    break
  fi
  sleep 2
done
if [[ "$healthy" != true ]]; then
  rollback
  exit 1
fi
# Verify that the running image contains the exact uploaded artifact.
expected=$(sha256sum "$stage/magazine-builder" | cut -d ' ' -f 1)
actual=$(docker exec website-magazine-builder-1 sha256sum /usr/local/bin/magazine-builder | cut -d ' ' -f 1)
if [[ "$expected" != "$actual" ]]; then
  rollback
  exit 1
fi
echo "Deployed magazine-builder sha256=$actual"
