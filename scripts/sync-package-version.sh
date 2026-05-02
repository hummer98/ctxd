#!/usr/bin/env bash
# Sync package.json version from .claude-plugin/plugin.json (the source of truth).
#
# CLAUDE.md ルールに従い、`.claude-plugin/plugin.json` の version が plugin の真のソース。
# package.json の version はそれと常に一致しなければならない。本スクリプトはその同期を行う。
#
# Usage:
#   bash scripts/sync-package-version.sh           # update package.json if mismatched (idempotent)
#   bash scripts/sync-package-version.sh --check   # exit 1 if mismatched (CI mode); never modifies files
#
# Exit codes:
#   0 = ok (versions match, or package.json was updated to match)
#   1 = mismatch detected in --check mode
#   2 = error (file missing, parse failure, etc.)

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PLUGIN_JSON="$ROOT_DIR/.claude-plugin/plugin.json"
PACKAGE_JSON="$ROOT_DIR/package.json"

if [[ ! -f "$PLUGIN_JSON" ]]; then
  echo "error: $PLUGIN_JSON not found" >&2
  exit 2
fi
if [[ ! -f "$PACKAGE_JSON" ]]; then
  echo "error: $PACKAGE_JSON not found" >&2
  exit 2
fi

CHECK_ONLY=0
if [[ "${1:-}" == "--check" ]]; then
  CHECK_ONLY=1
elif [[ -n "${1:-}" ]]; then
  echo "error: unknown argument: $1 (use --check or no args)" >&2
  exit 2
fi

extract_version() {
  # extract first "version": "X.Y.Z" occurrence
  grep -E '^[[:space:]]*"version"[[:space:]]*:' "$1" \
    | head -n1 \
    | sed -E 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/'
}

plugin_version=$(extract_version "$PLUGIN_JSON")
package_version=$(extract_version "$PACKAGE_JSON")

if [[ -z "$plugin_version" ]]; then
  echo "error: failed to extract version from $PLUGIN_JSON" >&2
  exit 2
fi
if [[ -z "$package_version" ]]; then
  echo "error: failed to extract version from $PACKAGE_JSON" >&2
  exit 2
fi

if [[ "$plugin_version" == "$package_version" ]]; then
  echo "ok: package.json version ($package_version) already matches plugin.json ($plugin_version)"
  exit 0
fi

if [[ "$CHECK_ONLY" -eq 1 ]]; then
  echo "mismatch: plugin.json=$plugin_version, package.json=$package_version" >&2
  echo "hint: run 'bash scripts/sync-package-version.sh' to update package.json" >&2
  exit 1
fi

# Replace only the FIRST "version" key in package.json (top-level).
# Use a temp file to avoid partial writes.
tmp="$PACKAGE_JSON.tmp.$$"
awk -v new="$plugin_version" '
  BEGIN { replaced = 0 }
  {
    if (!replaced && match($0, /"version"[[:space:]]*:[[:space:]]*"[^"]+"/)) {
      sub(/"version"[[:space:]]*:[[:space:]]*"[^"]+"/, "\"version\": \"" new "\"")
      replaced = 1
    }
    print
  }
' "$PACKAGE_JSON" > "$tmp"
mv "$tmp" "$PACKAGE_JSON"
echo "updated: package.json version $package_version -> $plugin_version"
