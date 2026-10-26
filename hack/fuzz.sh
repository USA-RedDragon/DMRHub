#!/bin/bash

set -e
files=$(grep -r --include='**_test.go' --files-with-matches 'func Fuzz' .)

for file in ${files}
do
    funcs=$(grep -oP 'func \K(Fuzz\w*)' $file)
    for func in ${funcs}
    do
        echo "Fuzzing $func in $file"
        parentDir=$(dirname $file)
        tags=codec2wasm
        if grep -q '^//go:build tinygo$' $file; then
            tags=$tags,tinygo
        fi
        go test -tags $tags $parentDir -run=$func -fuzz=$func -fuzztime=5m -fuzzminimizetime=5m
    done
done
