#!/usr/bin/env zsh
# Build and (re)start grook with podman compose. Extra args go to `up`.
set -euo pipefail
cd "${0:A:h}"

if [[ ! -f .env ]]; then
  print -u2 "run.sh: missing .env (copy .env.example and fill it in)"
  exit 1
fi
if ! podman compose version &>/dev/null; then
  print -u2 "run.sh: no compose provider for podman; install one with: sudo dnf install podman-compose"
  exit 1
fi

podman compose up -d --build "$@"
podman compose ps
print "Follow logs with: podman compose logs -f"
