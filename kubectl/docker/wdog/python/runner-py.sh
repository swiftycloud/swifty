#!/bin/sh
export RUNNERAPI="1"
exec /usr/local/bin/python3 -u /usr/bin/swy-runner.py "script"$1
