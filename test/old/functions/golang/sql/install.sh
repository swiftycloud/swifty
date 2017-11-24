#!/bin/sh
DEP="github.com/go-sql-driver/mysql"
GOPATH=`pwd` go get -d ${DEP}
mv src vendor
rm -rf vendor/$DEP/.git
