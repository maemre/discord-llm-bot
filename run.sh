#!/usr/bin/env zsh
# Build the grook image and (re)start its container with plain podman.
set -euo pipefail
cd "${0:A:h}"

if [[ ! -f .env ]]; then
  print -u2 "run.sh: missing .env (copy .env.example and fill it in)"
  exit 1
fi

podman build -t grook .
podman rm -f --ignore grook >/dev/null
podman run -d --name grook --env-file .env --restart unless-stopped grook
podman ps --filter name=grook
print "Follow logs with: podman logs -f grook"
