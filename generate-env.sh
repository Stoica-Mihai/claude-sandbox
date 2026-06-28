#!/usr/bin/env sh
set -eu

# Scoped, host-isolated Claude config dir for the sandbox (mounted into the
# backend). Create it up front so Docker doesn't create it root-owned.
mkdir -p "${HOME}/.claude-sandbox"

if [ -f .env ]; then
    echo ".env already exists, skipping (delete it first to regenerate)"
    exit 0
fi

cp .env.example .env
sed -i "s/^UID=.*/UID=$(id -u)/" .env
sed -i "s/^GID=.*/GID=$(id -g)/" .env

echo ".env created with UID=$(id -u) and GID=$(id -g)"
