# mag.jonh.no deployment

The production service is part of `/root/website/docker-compose.yml` on the jonh.no server. nginx proxies to port 8080. Workspace data remains in `/root/website/magazine-builder/data` and uses the existing 24-hour retention setting.

GitHub Actions checks pull requests. Pushes to `master` and manual dispatch on `master` also upload the tested Linux amd64 binary to `magazine-deploy`. Configure repository secrets `DEPLOY_KEY` (dedicated private key), `DEPLOY_KNOWN_HOSTS` (verified server host key), and variable `DEPLOY_HOST` (server address). The forced command consumes binary bytes on stdin; it does not accept remote shell commands or uploaded scripts.

## Server bootstrap

Install `deploy-server.sh` as root-owned `/usr/local/sbin/deploy-magazine-builder` (0755). Copy `Dockerfile.runtime` and `compose.deploy.yml` into `/root/website/magazine-builder/`, owned by root. These control files are administered separately from application deployments.

Tag the currently running image as the runtime base, preserving its installed defapi and dependencies:

```sh
docker tag "$(docker inspect --format '{{.Image}}' website-magazine-builder-1)" magazine-builder-runtime:base
```

Create a dedicated `magazine-deploy` account with `/bin/bash`, a root-owned home at `/var/lib/magazine-deploy`, and a root-owned `.ssh/authorized_keys`. Prefix its dedicated public key with:

```text
restrict,command="sudo -n /usr/local/sbin/deploy-magazine-builder"
```

The root-owned sudoers file `/etc/sudoers.d/magazine-deploy` contains only:

```text
magazine-deploy ALL=(root) NOPASSWD: /usr/local/sbin/deploy-magazine-builder ""
```

Validate with `visudo -cf /etc/sudoers.d/magazine-deploy`. Do not grant Docker group membership or unrestricted sudo. Obtain the host public key over the existing trusted administration connection before configuring GitHub's known-hosts secret.

The base image is intentionally retained locally. Update `magazine-builder-runtime:base` deliberately when upgrading Alpine or defapi; application deploys do not download an unrelated magazine release or change defapi. Back up the data and runtime image as part of server maintenance.

## Deployment and rollback

The helper serializes deployments, builds a candidate image, checks that the binary starts, recreates only this service, checks HTTP availability, and verifies its SHA-256 against the upload. Failure restores `magazine-builder:previous`. Restarting interrupts active generation tasks; deploy between builds when practical.

For an explicit rollback on the server:

```sh
cd /root/website
docker tag magazine-builder:previous magazine-builder:production
docker compose -f docker-compose.yml -f magazine-builder/compose.deploy.yml up -d --no-deps --no-build --force-recreate magazine-builder
```

Use both Compose files for manual service maintenance. A later push to master deploys the new commit again. GitHub also compares the public `/static/app.js` against the checked-out source. Public-proxy verification failures fail the workflow; inspect nginx/networking separately if the service-level checks passed.

`deploy/docker-compose.yml`, `Dockerfile`, and `update.sh` are the older standalone release-installation route, separate from this production workflow.
