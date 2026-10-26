// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

//go:build tinygo

package pluginv1_test

import (
	"bytes"
	_ "embed"
	"math"
	"math/rand/v2"
	"testing"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

//go:embed testdata/plugin.binpb
var descriptor []byte

// lite is a message that protoc-gen-go-lite generates.
type lite interface {
	MarshalVT() ([]byte, error)
	UnmarshalVT(b []byte) error
	SizeVT() int
}

// lites are the messages of the protocol in the TinyGo build.
var lites = map[protoreflect.FullName]func() lite{
	"dmrhub.plugin.v1.ConnectRequest":  func() lite { return &pluginv1.ConnectRequest{} },
	"dmrhub.plugin.v1.Hello":           func() lite { return &pluginv1.Hello{} },
	"dmrhub.plugin.v1.Host":            func() lite { return &pluginv1.Host{} },
	"dmrhub.plugin.v1.NetRequest":      func() lite { return &pluginv1.NetRequest{} },
	"dmrhub.plugin.v1.NetClose":        func() lite { return &pluginv1.NetClose{} },
	"dmrhub.plugin.v1.NetResponse":     func() lite { return &pluginv1.NetResponse{} },
	"dmrhub.plugin.v1.Subscribe":       func() lite { return &pluginv1.Subscribe{} },
	"dmrhub.plugin.v1.Position":        func() lite { return &pluginv1.Position{} },
	"dmrhub.plugin.v1.ConnectResponse": func() lite { return &pluginv1.ConnectResponse{} },
	"dmrhub.plugin.v1.Draining":        func() lite { return &pluginv1.Draining{} },
	"dmrhub.plugin.v1.Tracked":         func() lite { return &pluginv1.Tracked{} },
	"dmrhub.plugin.v1.Tracking":        func() lite { return &pluginv1.Tracking{} },
	"dmrhub.plugin.v1.Heard":           func() lite { return &pluginv1.Heard{} },
	"dmrhub.plugin.v1.Welcome":         func() lite { return &pluginv1.Welcome{} },
	"dmrhub.plugin.v1.Ranges":          func() lite { return &pluginv1.Ranges{} },
	"dmrhub.plugin.v1.IDRange":         func() lite { return &pluginv1.IDRange{} },
	"dmrhub.plugin.v1.Burst":           func() lite { return &pluginv1.Burst{} },
	"dmrhub.plugin.v1.Refusal":         func() lite { return &pluginv1.Refusal{} },
}

func messages(t testing.TB) []protoreflect.MessageDescriptor {
	t.Helper()
	var set descriptorpb.FileDescriptorSet
	require.NoError(t, proto.Unmarshal(descriptor, &set))
	files, err := protodesc.NewFiles(&set)
	require.NoError(t, err)
	var out []protoreflect.MessageDescriptor
	files.RangeFiles(func(f protoreflect.FileDescriptor) bool {
		for i := range f.Messages().Len() {
			out = append(out, f.Messages().Get(i))
		}
		return true
	})
	return out
}

func TestEveryMessageHasALiteMessage(t *testing.T) {
	t.Parallel()
	ms := messages(t)
	require.Len(t, lites, len(ms))
	for _, md := range ms {
		require.Contains(t, lites, md.FullName())
	}
}

// fill sets each field of m to a random value, or leaves it unset.
func fill(r *rand.Rand, m protoreflect.Message, depth int) {
	fields := m.Descriptor().Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		if r.IntN(3) == 0 {
			continue
		}
		switch {
		case fd.IsList():
			l := m.Mutable(fd).List()
			for range r.IntN(4) {
				if fd.Kind() == protoreflect.MessageKind {
					if depth <= 0 {
						break
					}
					e := l.NewElement()
					fill(r, e.Message(), depth-1)
					l.Append(e)
					continue
				}
				l.Append(scalar(r, fd))
			}
		case fd.Kind() == protoreflect.MessageKind:
			if depth <= 0 {
				continue
			}
			sub := m.NewField(fd)
			fill(r, sub.Message(), depth-1)
			m.Set(fd, sub)
		default:
			m.Set(fd, scalar(r, fd))
		}
	}
}

func scalar(r *rand.Rand, fd protoreflect.FieldDescriptor) protoreflect.Value {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(r.IntN(2) == 0)
	case protoreflect.EnumKind:
		return protoreflect.ValueOfEnum(protoreflect.EnumNumber(int32(r.Uint32()) >> r.IntN(32)))
	case protoreflect.Uint32Kind:
		return protoreflect.ValueOfUint32(r.Uint32() >> r.IntN(32))
	case protoreflect.Int64Kind:
		return protoreflect.ValueOfInt64(int64(r.Uint64()) >> r.IntN(64))
	case protoreflect.DoubleKind:
		switch r.IntN(6) {
		case 0:
			return protoreflect.ValueOfFloat64(0)
		case 1:
			return protoreflect.ValueOfFloat64(math.Inf(1 - 2*r.IntN(2)))
		case 2:
			return protoreflect.ValueOfFloat64(math.NaN())
		default:
			return protoreflect.ValueOfFloat64((r.Float64() - 0.5) * math.Pow(10, float64(r.IntN(20)-10)))
		}
	case protoreflect.StringKind:
		b := make([]rune, r.IntN(12))
		for i := range b {
			b[i] = rune(r.IntN(0x2000))
		}
		return protoreflect.ValueOfString(string(b))
	case protoreflect.BytesKind:
		b := make([]byte, r.IntN(40))
		for i := range b {
			b[i] = byte(r.Uint32())
		}
		return protoreflect.ValueOfBytes(b)
	default:
		panic("a kind that the protocol does not use: " + fd.Kind().String())
	}
}

var deterministic = proto.MarshalOptions{Deterministic: true}

// TestLiteMessagesEncodeLikeProtobuf fills each message of the protocol
// at random, encodes it with google.golang.org/protobuf, and checks that
// the lite message decodes it and encodes the same bytes again.
func TestLiteMessagesEncodeLikeProtobuf(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(1, 2))
	for _, md := range messages(t) {
		for range 2000 {
			m := dynamicpb.NewMessage(md)
			fill(r, m, 3)
			want, err := deterministic.Marshal(m)
			require.NoError(t, err)
			l := lites[md.FullName()]()
			require.NoError(t, l.UnmarshalVT(want), md.FullName())
			require.Equal(t, len(want), l.SizeVT(), md.FullName())
			got, err := l.MarshalVT()
			require.NoError(t, err)
			require.Equal(t, want, got, md.FullName())
		}
	}
}

// TestLiteMessagesDropANegativeZero shows the one place where the lite
// messages encode other bytes than google.golang.org/protobuf: a double
// without presence that is -0.0, which protobuf sends and the lite
// messages leave out, as 0.
func TestLiteMessagesDropANegativeZero(t *testing.T) {
	t.Parallel()
	want, err := (&pluginv1.Position{Latitude: math.Copysign(0, -1)}).MarshalVT()
	require.NoError(t, err)
	require.Empty(t, want)
}

// FuzzLiteMessagesDecodeLikeProtobuf decodes the same bytes as each
// message of the protocol with google.golang.org/protobuf and with the
// lite message, and checks that the lite message takes what protobuf
// takes, and encodes the same bytes again. The lite message refuses a field
// of the wrong wire type, which protobuf keeps as an unknown field, and
// takes some bytes that protobuf refuses, such as strings that are not
// UTF-8 and field numbers out of range, which the hub never sends.
func FuzzLiteMessagesDecodeLikeProtobuf(f *testing.F) {
	ms := messages(f)
	r := rand.New(rand.NewPCG(3, 4))
	for _, md := range ms {
		for range 20 {
			m := dynamicpb.NewMessage(md)
			fill(r, m, 3)
			b, err := deterministic.Marshal(m)
			require.NoError(f, err)
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, md := range ms {
			m := dynamicpb.NewMessage(md)
			perr := proto.Unmarshal(b, m)
			l := lites[md.FullName()]()
			lerr := l.UnmarshalVT(b)
			if perr != nil {
				continue
			}
			if lerr != nil {
				require.Contains(t, lerr.Error(), "wrong wireType", md.FullName())
				continue
			}
			want, err := deterministic.Marshal(m)
			require.NoError(t, err)
			got, err := l.MarshalVT()
			require.NoError(t, err)
			if !bytes.Equal(want, got) {
				require.True(t, negativeZero(m), "%s: %x != %x", md.FullName(), want, got)
			}
		}
	})
}

// negativeZero reports whether a double without presence in m is -0.0.
func negativeZero(m protoreflect.Message) bool {
	found := false
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.Kind() == protoreflect.DoubleKind && !fd.HasPresence() && !fd.IsList():
			found = math.Signbit(v.Float()) && v.Float() == 0
		case fd.Kind() == protoreflect.MessageKind && fd.IsList():
			for i := range v.List().Len() {
				found = found || negativeZero(v.List().Get(i).Message())
			}
		case fd.Kind() == protoreflect.MessageKind:
			found = negativeZero(v.Message())
		}
		return !found
	})
	return found
}
