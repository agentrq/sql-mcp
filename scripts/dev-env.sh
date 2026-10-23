#!/bin/sh
# Writes backend/.env for local development when there is none: fresh
# secrets, root sign-in on, and the Vite dev server as the base URL. An
# existing file is never touched.
set -eu
cd "$(dirname "$0")/.."
env_file=backend/.env
[ -f "$env_file" ] && exit 0

rand_hex() {
  if command -v openssl >/dev/null 2>&1; then openssl rand -hex "$1"
  else od -An -tx1 -N"$1" /dev/urandom | tr -d ' \n'; fi
}

root_token=$(rand_hex 16)
umask 077
sed \
  -e "s|^APP_BASE_URL=.*|APP_BASE_URL=http://localhost:5173|" \
  -e "s|^JWT_SECRET=.*|JWT_SECRET=$(rand_hex 32)|" \
  -e "s|^ENCRYPTION_KEY=.*|ENCRYPTION_KEY=$(rand_hex 32)|" \
  -e "s|^ALLOWED_EMAILS=.*|ALLOWED_EMAILS=root@sqlmcp.local|" \
  -e "s|^ROOT_LOGIN_ENABLED=.*|ROOT_LOGIN_ENABLED=true|" \
  -e "s|^ROOT_TOKEN=.*|ROOT_TOKEN=$root_token|" \
  .env.example > "$env_file"

cat <<MSG

  Created $env_file for local development.
  Sign in at http://localhost:5173/login, or open the admin setup at
  http://localhost:5173/admin, with the root token:

      $root_token

  (It is ROOT_TOKEN in $env_file. Edit that file for Google sign-in.)

MSG
