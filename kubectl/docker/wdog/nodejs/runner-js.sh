#!/bin/sh
export RUNNERAPI="1"
export NODE_PATH=/home/packages/node_modules
exec node /home/swifty/runner.js "script"$1
