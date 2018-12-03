#!/bin/sh
export RUNNERAPI="1"
export MONO_PATH="/mono/runner"
exec "/usr/bin/mono" "/function/runner${1}.exe"
