#!/bin/bash

if ! [ -d bin ]; then mkdir bin; fi;

export GOPATH=`pwd`

go get

for GOOS in linux; do
    if ! [ -d bin/$GOOS ]; then mkdir bin/$GOOS; fi;
    for GOARCH in 386 amd64 arm arm64; do
        echo "Building $GOARCH for system $GOOS"
        export GOOS=$GOOS
        export GOARCH=$GOARCH
        go build -o bin/$GOOS/eir-$GOARCH
    done
done
