#!/bin/sh
# Install the TinyGo that builds the plugins that come with DMRHub, and put
# it on the PATH of the next steps of a GitHub Actions job.
set -eu
version=0.43.0
sha256=ca9fc6d055e3889462dea4450a2e42a92242d88b5c362a7ec39bdb563413d78a
dir="${RUNNER_TEMP:-$HOME}/tinygo"
curl -fsSL -o "$dir.tar.gz" "https://github.com/tinygo-org/tinygo/releases/download/v$version/tinygo$version.linux-amd64.tar.gz"
echo "$sha256  $dir.tar.gz" | sha256sum -c -
mkdir -p "$dir"
tar -xzf "$dir.tar.gz" -C "$dir" --strip-components=1
rm "$dir.tar.gz"
echo "$dir/bin" >> "$GITHUB_PATH"
