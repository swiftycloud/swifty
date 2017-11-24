#!/bin/sh
DEP="github.com/streadway/amqp"
GOPATH=`pwd` go get -u ${DEP}
mv src vendor
rm -rf vendor/$DEP/.git
