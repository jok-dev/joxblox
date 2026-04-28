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

METADATA_FILE="internal/app/app_metadata.go"
if ! grep -Eq '^var appVersion = "[^"]+"$' "$METADATA_FILE"; then
  echo "Aborting: could not find 'var appVersion = \"...\"' in $METADATA_FILE." >&2
  exit 1
fi

CHANGELOG_FILE="CHANGELOG.md"
if [ ! -f "$CHANGELOG_FILE" ]; then
  echo "Aborting: $CHANGELOG_FILE not found." >&2
  exit 1
fi
if ! grep -Eq '^## Unreleased$' "$CHANGELOG_FILE"; then
  echo "Aborting: could not find '## Unreleased' heading in $CHANGELOG_FILE." >&2
  exit 1
fi

TODAY="$(date +%Y-%m-%d)"

# Rewrite the Unreleased section: keep '## Unreleased' as a fresh empty
# placeholder for the next cycle, and insert '## <version> - <date>'
# directly under it so the existing bullet list moves into the new
# release. Only acts on the first occurrence.
TMP_CHANGELOG="${CHANGELOG_FILE}.bump.tmp"
awk -v version="$NEW_VERSION" -v today="$TODAY" '
  BEGIN { renamed = 0 }
  !renamed && /^## Unreleased$/ {
    print "## Unreleased"
    print ""
    print "## " version " - " today
    renamed = 1
    next
  }
  { print }
' "$CHANGELOG_FILE" > "$TMP_CHANGELOG"
mv "$TMP_CHANGELOG" "$CHANGELOG_FILE"

if ! grep -Fq "## ${NEW_VERSION} - ${TODAY}" "$CHANGELOG_FILE"; then
  echo "Aborting: changelog rewrite did not apply to $CHANGELOG_FILE." >&2
  exit 1
fi

# Portable in-place rewrite (BSD sed on macOS rejects bare -i).
TMP_METADATA="${METADATA_FILE}.bump.tmp"
sed -E "s|^var appVersion = \"[^\"]+\"$|var appVersion = \"${NEW_VERSION}\"|" "$METADATA_FILE" > "$TMP_METADATA"
mv "$TMP_METADATA" "$METADATA_FILE"

if ! grep -Fq "var appVersion = \"${NEW_VERSION}\"" "$METADATA_FILE"; then
  echo "Aborting: version bump did not apply to $METADATA_FILE." >&2
  exit 1
fi

git add "$METADATA_FILE" "$CHANGELOG_FILE"
git commit -m "Prepare ${NEW_VERSION} release"
git tag "${NEW_VERSION}"
git push origin "${BRANCH}"
git push origin "${NEW_VERSION}"

echo "Released ${NEW_VERSION} (bumped $METADATA_FILE + $CHANGELOG_FILE, commit + tag pushed on ${BRANCH})."
