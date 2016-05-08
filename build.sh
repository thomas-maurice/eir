#!/bin/bash

if ! [ -d bin ]; then mkdir bin; fi;

export GOPATH=`pwd`

go get

echo -e "package main

var WebUITemplate = \`
`cat webui.html`
\`
" > webui.go

if ! [ -z "$1" ] && ! [ -z "$2" ]; then
    export GOOS=$1
    export GOARCH=$2
    go build
    exit
fi;

for GOOS in linux; do
    for GOARCH in 386 amd64 arm arm64; do
        echo "Building $GOARCH for system $GOOS"
        export GOOS=$GOOS
        export GOARCH=$GOARCH
        go build -o bin/eir-${GOOS}-$GOARCH
    done
done
