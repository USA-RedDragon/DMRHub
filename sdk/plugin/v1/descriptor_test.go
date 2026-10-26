// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

//go:build !tinygo

package pluginv1_test

import (
	_ "embed"
	"testing"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
)

//go:embed testdata/plugin.binpb
var descriptor []byte

// TestTheDescriptorOfTheLiteTestsIsCurrent checks that
// testdata/plugin.binpb, which the tests of the TinyGo build compare the
// lite messages with, describes the protocol of plugin.pb.go. make proto
// writes it.
func TestTheDescriptorOfTheLiteTestsIsCurrent(t *testing.T) {
	t.Parallel()
	var set descriptorpb.FileDescriptorSet
	require.NoError(t, proto.Unmarshal(descriptor, &set))
	require.Len(t, set.GetFile(), 1)
	want := protodesc.ToFileDescriptorProto(pluginv1.File_dmrhub_plugin_v1_plugin_proto)
	require.True(t, proto.Equal(want, set.GetFile()[0]), "run make proto")
}
