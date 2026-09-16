#!/usr/bin/env bash
# Database backup; keep .env separately in secure storage to retain signing/auth keys.
set -euo pipefail
umask 077
repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"
mkdir -p backups
output=${1:-"backups/fact0-$(date -u +%Y%m%dT%H%M%SZ).dump"}
if [[ -e "$output" ]]; then
  printf '%s\n' 'Backup output already exists; choose a new filename.' >&2
  exit 1
fi
target_dir=$(cd "$(dirname "$output")" && pwd)
output="$target_dir/$(basename "$output")"
temporary=$(mktemp "$target_dir/.fact0-backup.XXXXXX")
trap 'rm -f "$temporary"' EXIT
docker compose exec -T postgres pg_dump -U fact0 -d fact0 --format=custom > "$temporary"
ln "$temporary" "$output" # atomic, same filesystem, and refuses existing files/symlinks
rm "$temporary"
printf 'Backup saved to %s. Back up .env separately and securely.\n' "$output"
