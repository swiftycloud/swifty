#!/bin/bash

set -e

SOURCES="/swift/swycode/${SWD_SOURCES}"
SCRIPT="/swift/runner/Sources/script.swift"

rm -f $SCRIPT
ln -s "${SOURCES}/script${SWD_SUFFIX}.swift" $SCRIPT

cd "/swift/runner"
swift build --build-path "../swycode/${SWD_SOURCES}"

rm -f $SCRIPT
mv "${SOURCES}/debug/function" "${SOURCES}/runner${SWD_SUFFIX}"
