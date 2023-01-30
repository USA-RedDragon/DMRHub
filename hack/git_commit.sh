#!/bin/bash

git fetch origin main:refs/remotes/origin/main --quiet
echo -n $(git rev-parse --short HEAD)
# Check if git is dirty because it differs from origin/main
# If it differs, echo -dirty
if [[ $(git diff origin/main) ]]; then
    echo -n "-dirty"
fi
