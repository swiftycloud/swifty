#!/bin/sh
export RUNNERAPI="1"
exec /usr/local/bin/ruby /home/swifty/runner.rb "script"$1
