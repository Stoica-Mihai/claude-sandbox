#!/usr/bin/env sh
set -eu

# Scoped, host-isolated Claude config dir for the sandbox (mounted into the
# backend). Create it up front so Docker doesn't create it root-owned.
mkdir -p "${HOME}/.claude-sandbox"

# Seed the (gitignored) container settings from the example on first run.
# docker-compose bind-mounts this file (read-write — the in-dashboard settings
# editor persists edits to it), so it must exist before `up`/`build`.
# Done before the .env early-exit so it still seeds on later runs if deleted.
if [ ! -f container-settings.json ]; then
    cp container-settings.example.json container-settings.json
    echo "container-settings.json created from container-settings.example.json"
fi

if [ -f .env ]; then
    echo ".env already exists, skipping (delete it first to regenerate)"
    exit 0
fi

cp .env.example .env
sed -i "s/^UID=.*/UID=$(id -u)/" .env
sed -i "s/^GID=.*/GID=$(id -g)/" .env

echo ".env created with UID=$(id -u) and GID=$(id -g)"
