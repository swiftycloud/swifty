#!/bin/bash

set -e

SOURCES="/go/src/swycode/${SWD_SOURCES}/script${SWD_SUFFIX}.go"
RNRDIR="/go/src/swyrunner"
SCRIPT="${RNRDIR}/script.go"
GOBODY="${RNRDIR}/body.go"

rm -f $SCRIPT
ln -s $SOURCES $SCRIPT

if ! go-sca -type $SCRIPT "Body"; then
	rm -f $GOBODY
	ln -s "body" $GOBODY
fi

cd $RNRDIR

if [ -n "$SWD_PACKAGES" ]; then
	export GOPATH="/go:${SWD_PACKAGES}"
fi

go build -o "../swycode/${SWD_SOURCES}/runner${SWD_SUFFIX}"

rm -f $SCRIPT
rm -f $GOBODY
