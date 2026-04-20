#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

usage() {
  cat <<'EOF'
Usage: ./prepare-release.sh [patch|minor|major|vX.Y.Z]

Bumps the version relative to the latest git tag, creates an empty
"Prepare <version> release" commit, tags it, and pushes both the
commit and the tag to origin.

  patch    (default) v1.2.3 -> v1.2.4
  minor              v1.2.3 -> v1.3.0
  major              v1.2.3 -> v2.0.0
  vX.Y.Z             use this exact version
EOF
}

BUMP="${1:-patch}"

case "$BUMP" in
  -h|--help)
    usage
    exit 0
    ;;
esac

if [ -n "$(git status --porcelain)" ]; then
  echo "Aborting: working tree is not clean. Commit, stash, or discard changes first." >&2
  git status --short >&2
  exit 1
fi

LATEST_TAG="$(git tag --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1 || true)"
if [ -z "$LATEST_TAG" ]; then
  echo "No existing vX.Y.Z tag found; pass an explicit version (e.g. v1.0.0)." >&2
  exit 1
fi

if [[ "$BUMP" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.].+)?$ ]]; then
  NEW_VERSION="$BUMP"
else
  CORE="${LATEST_TAG#v}"
  IFS='.' read -r MAJOR MINOR PATCH <<< "$CORE"
  case "$BUMP" in
    patch) PATCH=$((PATCH + 1));;
    minor) MINOR=$((MINOR + 1)); PATCH=0;;
    major) MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0;;
    *)
      echo "Unknown bump type: $BUMP" >&2
      usage >&2
      exit 1
      ;;
  esac
  NEW_VERSION="v${MAJOR}.${MINOR}.${PATCH}"
fi

if git rev-parse --verify --quiet "refs/tags/${NEW_VERSION}" >/dev/null; then
  echo "Aborting: tag ${NEW_VERSION} already exists." >&2
  exit 1
fi

BRANCH="$(git rev-parse --abbrev-ref HEAD)"

echo "Branch:      ${BRANCH}"
echo "Latest tag:  ${LATEST_TAG}"
echo "New version: ${NEW_VERSION}"
printf 'Proceed with commit + tag + push? [y/N] '
read -r CONFIRM
case "$CONFIRM" in
  y|Y|yes|YES) ;;
  *) echo "Aborted."; exit 1;;
esac

git commit --allow-empty -m "Prepare ${NEW_VERSION} release"
git tag "${NEW_VERSION}"
git push origin "${BRANCH}"
git push origin "${NEW_VERSION}"

echo "Released ${NEW_VERSION} (commit + tag pushed on ${BRANCH})."
