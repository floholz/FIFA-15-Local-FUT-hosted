#!/usr/bin/env bash
# Reliable VPS redeploy: pull latest and force a clean image rebuild.
# `docker compose up -d --build` sometimes serves a stale cached layer; this
# sequence always picks up new code. Run from the repo dir on the VPS.
set -euo pipefail
git pull
docker compose build --no-cache
docker compose up -d --force-recreate
echo "--- deployed:"; git log --oneline -1
