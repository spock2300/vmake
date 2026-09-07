#!/bin/sh
set -e

cd "$(dirname "$0")"

VMAKE=${VMAKE:-../../vmake}
REPO_NAME=subtest

if [ ! -d .fixture/mother.git ]; then
	rm -rf .fixture
	mkdir -p .fixture
	cp -r .fixture-src/mother .fixture/work
	git -C .fixture/work init -q
	git -C .fixture/work add .
	git -C .fixture/work -c user.name=vmake -c user.email=vmake@test.local commit -qm "fixture"
	git -C .fixture/work tag v1.0.0
	git -C .fixture/work clone -q --bare . "$(pwd)/.fixture/mother.git"
	rm -rf .fixture/work
fi

URL="file://$(pwd)/.fixture/{name}.git"
if [ ! -f "$HOME/.vmake/repos/$REPO_NAME/repo.json" ]; then
	$VMAKE repo add --native "$REPO_NAME" "$URL"
	$VMAKE repo trust "$REPO_NAME"
fi
