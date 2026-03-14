#!/usr/bin/env sh

if [ -f .env ]; then
    echo ".env already exists, skipping (delete it first to regenerate)"
    exit 0
fi

cp .env.example .env
sed -i "s/^UID=.*/UID=$(id -u)/" .env
sed -i "s/^GID=.*/GID=$(id -g)/" .env

echo ".env created with UID=$(id -u) and GID=$(id -g)"
