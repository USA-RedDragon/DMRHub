#!/bin/bash

set -e

files=$(grep -r --include='**_test.go' --files-with-matches 'func Fuzz' .)
pids=()
failed=0

for file in ${files}
do
    funcs=$(grep -oP 'func \K(Fuzz\w*)' $file)
    for func in ${funcs}
    do
        echo "Starting fuzz: $func in $file"
        parentDir=$(dirname $file)
        go test $parentDir -run=$func -fuzz=$func -fuzztime=1m -fuzzminimizetime=1m &
        pids+=("$!:$func:$file")
    done
done

echo ""
echo "Waiting for ${#pids[@]} fuzz jobs..."

for entry in "${pids[@]}"
do
    IFS=: read -r pid func file <<< "$entry"
    if wait "$pid"; then
        echo "PASS: $func ($file)"
    else
        echo "FAIL: $func ($file)"
        failed=1
    fi
done

if [ "$failed" -ne 0 ]; then
    echo ""
    echo "Some fuzz tests failed!"
    exit 1
fi

echo ""
echo "All fuzz tests passed!"
