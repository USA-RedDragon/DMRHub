// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package plugin_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func wasmGo(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestTheWasmBuildLeavesGRPCOut(t *testing.T) {
	t.Parallel()
	for dep := range strings.FieldsSeq(wasmGo(t, "list", "-deps", "-f", "{{.ImportPath}}", ".")) {
		if strings.HasPrefix(dep, "google.golang.org/grpc") {
			t.Fatalf("a plugin built for wasip1 links %s", dep)
		}
	}
}

func TestAPluginBuildsForWasm(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "parrot.wasm")
	wasmGo(t, "build", "-o", out, "../../plugins/parrot/cmd/parrot")
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		t.Fatalf("no wasm: %v", err)
	}
}
