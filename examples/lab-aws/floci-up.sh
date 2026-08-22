#!/bin/sh
# Start Floci (a local AWS emulator) so the lab-aws sandbox can reach it.
#
# Floci serves AWS APIs on port 4566. Its in-process services (S3, DynamoDB,
# EC2, VPC, ...) need nothing special. Lambda is different: Floci spawns real
# containers for it, so it needs a container socket and its Lambda containers
# must share a network with it, or their callback to the Runtime API fails.
#
# This handles both, for a rootless podman host. On a Docker host, plain
# `docker run -p 4566:4566 -v /var/run/docker.sock:/var/run/docker.sock
# floci/floci:latest` is enough.
set -eu

ENGINE=${ENGINE:-podman}
NET=flocinet

if [ "$ENGINE" = podman ]; then
  # Floci talks the Docker API; expose podman's compatible socket.
  SOCK="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/podman/podman.sock"
  systemctl --user start podman.socket 2>/dev/null || true
  [ -S "$SOCK" ] || { podman system service --time=0 "unix://$SOCK" >/dev/null 2>&1 & sleep 2; }
else
  SOCK=/var/run/docker.sock
fi

$ENGINE network create "$NET" 2>/dev/null || true
$ENGINE rm -f floci 2>/dev/null || true

# FLOCI_SERVICES_LAMBDA_DOCKER_NETWORK + FLOCI_HOSTNAME put the Lambda
# containers on the same network and give them a name to call back to, which
# is what makes Lambda work under podman.
$ENGINE run -d --name floci --network "$NET" -p 4566:4566 \
  -v "$SOCK:/var/run/docker.sock" \
  -e FLOCI_SERVICES_LAMBDA_DOCKER_NETWORK="$NET" \
  -e FLOCI_HOSTNAME=floci \
  docker.io/floci/floci:latest

echo "Floci is up on http://localhost:4566"
echo "From the sandbox it is reachable at http://host.containers.internal:4566"
