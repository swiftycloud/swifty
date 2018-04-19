#!/bin/bash

ID=${1}
CODE="./test/functions/golang/simple-user-mgmt.go"

cat contrib/auth-dep.json | sed -e "s/#NAME#/${ID}/g" -e "s/#CODE#/$(cat ${CODE} | base64 -w 0)/" > auth-dep.json
echo "Run \"swyctl ds auth-dep.json\""
