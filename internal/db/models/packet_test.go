package models_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/google/go-cmp/cmp"
)

var knownGoodPacketBytes = []byte{68, 77, 82, 68, 1, 0, 0, 2, 0, 0, 3, 0, 0, 0, 4, 165, 0, 0, 0, 6, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 7, 8}
var knownGoodPacket = models.Packet{
	Signature:   "DMRD",
	Seq:         1,
	Src:         2,
	Dst:         3,
	Repeater:    4,
	Slot:        true,
	GroupCall:   true,
	FrameType:   dmrconst.FrameDataSync,
	DTypeOrVSeq: 5,
	StreamID:    6,
	BER:         7,
	RSSI:        8,
	DMRData:     [33]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33},
}

func TestDecode(t *testing.T) {
	t.Parallel()
	if !cmp.Equal(knownGoodPacket, models.UnpackPacket(knownGoodPacketBytes)) {
		t.Errorf("Packet did not decode properly")
	}
	t.Log(knownGoodPacket.String())
}

func TestEncode(t *testing.T) {
	t.Parallel()
	if !cmp.Equal(knownGoodPacketBytes, knownGoodPacket.Encode()) {
		t.Errorf("Packet did not encode properly")
	}
}

var knownGoodPacketBytesNoRSSIandBER = []byte{68, 77, 82, 68, 1, 0, 0, 2, 0, 0, 3, 0, 0, 0, 4, 165, 0, 0, 0, 6, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33}

func TestPacketWithoutRSSIandBERIsNegOne(t *testing.T) {
	t.Parallel()
	p := models.UnpackPacket(knownGoodPacketBytesNoRSSIandBER)
	if p.RSSI != -1 || p.BER != -1 {
		t.Errorf("Packet did not decode properly")
	}
}

var knownGoodPacketPrivate = []byte{68, 77, 82, 68, 1, 0, 0, 2, 0, 0, 3, 0, 0, 0, 4, 229, 0, 0, 0, 6, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 7, 8}
var knownGoodPrivatePacket = models.Packet{
	Signature:   "DMRD",
	Seq:         1,
	Src:         2,
	Dst:         3,
	Repeater:    4,
	Slot:        true,
	GroupCall:   false,
	FrameType:   dmrconst.FrameDataSync,
	DTypeOrVSeq: 5,
	StreamID:    6,
	BER:         7,
	RSSI:        8,
	DMRData:     [33]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33},
}

func TestPacketPrivateDecode(t *testing.T) {
	t.Parallel()
	p := models.UnpackPacket(knownGoodPacketPrivate)
	if p.GroupCall {
		t.Errorf("Packet did not decode properly")
	}
}

func TestPacketPrivateEncode(t *testing.T) {
	t.Parallel()
	if !cmp.Equal(knownGoodPrivatePacket.Encode(), knownGoodPacketPrivate) {
		t.Errorf("Packet did not encode properly")
	}
}

func BenchmarkEncodeHomebrewPacket(b *testing.B) {
	b.StopTimer()
	p := models.Packet{}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		p.Encode()
	}
}

func BenchmarkDecodeHomebrewPacket(b *testing.B) {
	b.StopTimer()
	p := models.Packet{}
	bytes := p.Encode()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		models.UnpackPacket(bytes)
	}
}
