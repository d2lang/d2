#!/bin/sh

# This variable is deliberately initialized instead of inherited. Cleanup must
# never remove a path supplied by the caller's environment.
NPM_AUTH_CONFIG=

create_npm_auth_config() {
  NPM_AUTH_CONFIG=$(mktemp "${TMPDIR:-/tmp}/d2-npm-auth.XXXXXX")
  chmod 600 "$NPM_AUTH_CONFIG"
  printf '//registry.npmjs.org/:_authToken=%s\n' "$NPM_TOKEN" >"$NPM_AUTH_CONFIG"
  unset NPM_TOKEN
}

remove_npm_auth_config() {
  if [ -n "$NPM_AUTH_CONFIG" ]; then
    rm -f "$NPM_AUTH_CONFIG"
    NPM_AUTH_CONFIG=
  fi
}
