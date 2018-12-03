#!/bin/bash
set -e

RUNNER="/mono/runner/runner.cs"
XSTREAM="/mono/runner/XStream.dll"
SOURCES="/mono/functions/${SWD_SOURCES}"
SCRIPT="${SOURCES}/script${SWD_SUFFIX}.cs"
BINARY="${SOURCES}/runner${SWD_SUFFIX}.exe"

csc $RUNNER $SCRIPT "-m:FR" "-r:${XSTREAM}" "-out:${BINARY}"
