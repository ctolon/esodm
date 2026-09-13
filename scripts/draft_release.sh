#!/usr/bin/env bash
set -euo pipefail

: "${RELEASE_TAG:?RELEASE_TAG is required}"
: "${GH_REPO:?GH_REPO is required}"
: "${GH_TOKEN:?GH_TOKEN is required}"

args=(--title "$RELEASE_TAG" --notes-file release-notes.md)
if [[ "$RELEASE_TAG" == *-* ]]; then
  args+=(--prerelease)
else
  args+=(--prerelease=false)
fi
if gh release view "$RELEASE_TAG" --json isDraft > existing-release.json; then
  python3 -c 'import json; assert json.load(open("existing-release.json"))["isDraft"], "Refusing to change a published release"'
  gh release edit "$RELEASE_TAG" "${args[@]}"
  gh release upload "$RELEASE_TAG" "esodm-${RELEASE_TAG}.tar.gz" SHA256SUMS --clobber
else
  gh release create "$RELEASE_TAG" "esodm-${RELEASE_TAG}.tar.gz" SHA256SUMS --verify-tag --draft "${args[@]}"
fi
