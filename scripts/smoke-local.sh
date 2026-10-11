#!/usr/bin/env bash
set -euo pipefail

work_dir="$(mktemp -d /tmp/projecthub-smoke.XXXXXX)"
api_pid=""
cleanup() {
  if [[ -n "$api_pid" ]]; then
    kill -TERM "$api_pid" 2>/dev/null || true
    wait "$api_pid" 2>/dev/null || true
  fi
  rm -rf -- "$work_dir"
}
trap cleanup EXIT

if [[ ! -s web/dist/index.html ]]; then
  npm --prefix web ci
  npm --prefix web run build
fi
mkdir -p internal/webapp/dist
cp -R web/dist/. internal/webapp/dist/
go build -o "$work_dir/projecthub-api" ./cmd/api
printf '%s\n' 'correct-horse-battery-staple' > "$work_dir/password"
chmod 600 "$work_dir/password"
export PROJECTHUB_DB_PATH="$work_dir/projecthub.db"
port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
export PROJECTHUB_LISTEN="127.0.0.1:$port"
base_url="http://127.0.0.1:$port"
"$work_dir/projecthub-api" bootstrap-admin --username smoke-admin --password-file "$work_dir/password"
"$work_dir/projecthub-api" > "$work_dir/api.log" 2>&1 &
api_pid=$!

ready=0
for _ in $(seq 1 50); do
  if curl -fsS "$base_url/healthz" > "$work_dir/health.json"; then
    ready=1
    break
  fi
  sleep 0.1
done
test "$ready" = 1
grep -q '"status":"ok"' "$work_dir/health.json"
curl -fsS "$base_url/" > "$work_dir/index.html"
grep -q 'Project Hub' "$work_dir/index.html"

curl -fsS -D "$work_dir/login.headers" -o "$work_dir/login.json" \
  -H "Origin: https://127.0.0.1:$port" \
  -H 'X-Forwarded-Proto: https' \
  -H 'Content-Type: application/json' \
  -d '{"username":"smoke-admin","password":"correct-horse-battery-staple"}' \
  "$base_url/api/v1/login"
csrf_token="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["csrfToken"])' "$work_dir/login.json")"
session_cookie="$(awk 'BEGIN{IGNORECASE=1} /^Set-Cookie: projecthub_session=/{sub(/^Set-Cookie: projecthub_session=/,"",$0); sub(/;.*/,"",$0); print; exit}' "$work_dir/login.headers")"
test -n "$csrf_token"
test -n "$session_cookie"

curl -fsS \
  -H "Origin: https://127.0.0.1:$port" \
  -H 'X-Forwarded-Proto: https' \
  -H "X-CSRF-Token: $csrf_token" \
  -H "Cookie: projecthub_session=$session_cookie" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Smoke Project","description":"isolated smoke","icon":"","category_id":"","sort_order":0,"links":[]}' \
  "$base_url/api/v1/projects" > "$work_dir/project.json"
grep -q 'Smoke Project' "$work_dir/project.json"

kill -TERM "$api_pid"
wait "$api_pid"
api_pid=""
printf '%s\n' 'isolated API smoke PASS: bootstrap, startup, healthz, SPA, login, authenticated mutation, graceful shutdown'
