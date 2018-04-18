#!/bin/bash

set -x

SWYCTL="./swyctl"
ID=${1}

[ -z "${ID}" ] && exit 1

AMGO="auth_${ID}_mgo"
AJWT="auth_${ID}_jwt"
AFUN="auth_${ID}"

$SWYCTL madd ${AMGO} mongo
$SWYCTL madd ${AJWT} authjwt
$SWYCTL add ${AFUN} -src "test/functions/golang/simple-user-mgmt.go" -event url -mw "${AMGO},${AJWT}" -tmo 3000
