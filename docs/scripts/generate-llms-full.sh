#!/usr/bin/env bash
# Concatenate Mintlify MDX pages into llms-full.txt for LLM tools.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/llms-full.txt"

{
  echo "# Fact0 - full documentation dump"
  echo "# Generated from *.mdx - do not edit by hand"
  echo ""
  find "$ROOT" -name '*.mdx' -not -path '*/node_modules/*' | sort | while read -r f; do
    rel="${f#$ROOT/}"
    echo "## ${rel%.mdx}"
    echo ""
    awk 'BEGIN{fm=0} /^---$/{fm++; if(fm==2){next}} fm<2{next} {print}' "$f"
    echo ""
    echo "---"
    echo ""
  done
} > "$OUT"
echo "Wrote $OUT"
