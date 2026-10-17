#!/bin/sh
set -e

cd "$(dirname "$0")"

VMAKE=${VMAKE:-../../vmake}
REPO_NAME=subtest

# Always rebuild the fixture so changes under .fixture-src take effect. The
# fixed commit metadata keeps the commit hash (and therefore build keys and
# compile_commands.json) stable across rebuilds.
rm -rf .fixture
mkdir -p .fixture
cp -r .fixture-src/mother .fixture/work
git -C .fixture/work init -q
git -C .fixture/work add .
GIT_AUTHOR_DATE="2000-01-01T00:00:00 +0000" \
GIT_COMMITTER_DATE="2000-01-01T00:00:00 +0000" \
git -C .fixture/work -c user.name=vmake -c user.email=vmake@test.local commit -qm "fixture"
git -C .fixture/work tag v1.0.0
git -C .fixture/work clone -q --bare . "$(pwd)/.fixture/mother.git"
rm -rf .fixture/work

URL="file://$(pwd)/.fixture/{name}.git"
if [ ! -f "$HOME/.vmake/repos/$REPO_NAME/repo.json" ]; then
	$VMAKE repo add --native "$REPO_NAME" "$URL"
	$VMAKE repo trust "$REPO_NAME"
fi
