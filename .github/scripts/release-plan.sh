#!/bin/sh
# Decide how the Release workflow should publish the VERSION file.
#
# The git tag and the GitHub Release are independent objects that can drift
# apart when a publish half-fails, so both facts are gathered separately:
#
#   tag      release   decision
#   absent   -         tag_and_publish: tag HEAD and publish the release
#   present  absent    publish_release_for_existing_tag, but only when the
#                      tag is HEAD or an ancestor of HEAD (recovery ships the
#                      tagged artifact; HEAD is never re-tagged)
#   present  present   noop: intentional no-op for merges without a bump
#   present  absent    exit 2 when the tag is foreign to HEAD's history: a
#                      human must decide; existing tags are never moved
#
# Emits key=value lines to $GITHUB_OUTPUT (or stdout when unset) and human
# logs to stderr. Validate changes with test-release-plan.sh, which drives
# this script against local fixture git repos and a stubbed gh — never a
# real release.
set -eu

out() {
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf '%s\n' "$1" >>"$GITHUB_OUTPUT"
  else
    printf '%s\n' "$1"
  fi
}

log() { printf '%s\n' "$*" >&2; }

if [ ! -f VERSION ]; then
  log "::error::VERSION file not found (run from the repository root)"
  exit 1
fi
ver="$(tr -d '[:space:]' < VERSION)"
if ! printf '%s' "$ver" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  log "::error::VERSION must contain a semver X.Y.Z string, got '$ver'"
  exit 1
fi
tag="v$ver"
out "version=$ver"
out "tag=$tag"

tag_exists=false
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null 2>&1; then
  tag_exists=true
fi

release_exists=false
if gh release view "$tag" >/dev/null 2>&1; then
  release_exists=true
fi

if [ "$tag_exists" = false ]; then
  out "decision=tag_and_publish"
  log "$tag does not exist: will tag HEAD and publish the release"
  exit 0
fi

if [ "$release_exists" = true ]; then
  out "decision=noop"
  log "$tag and its GitHub release already exist: intentional no-op"
  exit 0
fi

# The tag exists but its release is missing — a previous publish half-failed.
# Recovery must publish the tagged artifact, so only proceed when the tag
# lies on HEAD's history; otherwise a human must resolve it deliberately.
if git merge-base --is-ancestor "refs/tags/$tag" HEAD; then
  out "decision=publish_release_for_existing_tag"
  log "$tag exists without a release and lies on HEAD's history: will publish the release for the tagged commit"
  exit 0
fi

log "::error::$tag exists without a release and does not point at HEAD or an ancestor of HEAD; refusing to move or reinterpret it. Inspect the tag, then publish or delete it deliberately."
exit 2
