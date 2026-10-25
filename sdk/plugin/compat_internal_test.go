// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package plugin

import (
	"slices"
	"testing"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// TestAnSDKThatDoesNotKnowDrainingReadsItAsNoMessage decodes a Draining
// with the protocol as it was before Draining, as an older SDK does.
func TestAnSDKThatDoesNotKnowDrainingReadsItAsNoMessage(t *testing.T) {
	t.Parallel()
	fdp := protodesc.ToFileDescriptorProto(pluginv1.File_dmrhub_plugin_v1_plugin_proto)
	fdp.MessageType = slices.DeleteFunc(fdp.MessageType, func(m *descriptorpb.DescriptorProto) bool { return m.GetName() == "Draining" })
	for _, m := range fdp.MessageType {
		if m.GetName() == "ConnectResponse" {
			m.Field = slices.DeleteFunc(m.Field, func(f *descriptorpb.FieldDescriptorProto) bool { return f.GetName() == "draining" })
		}
	}
	old, err := protodesc.NewFile(fdp, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	b, err := proto.Marshal(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Draining{Draining: &pluginv1.Draining{}}})
	if err != nil {
		t.Fatal(err)
	}
	md := old.Messages().ByName("ConnectResponse")
	m := dynamicpb.NewMessage(md)
	if err := proto.Unmarshal(b, m); err != nil {
		t.Fatalf("an older SDK cannot read a Draining: %v", err)
	}
	if f := m.WhichOneof(md.Oneofs().ByName("message")); f != nil {
		t.Fatalf("an older SDK reads a Draining as %s", f.Name())
	}
}
