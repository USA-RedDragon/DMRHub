// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package dmr_test

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/fec"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var updateGolden = flag.Bool("update", false, "rewrite the expected rows in testdata/golden from the rows the hub wrote") //nolint:gochecknoglobals

// A test that needs a commit of pendingCommits skips and names it. Remove
// the entry when the commit lands; DMRHUB_RUN_PENDING=1 runs them all.
func pendingCommits() map[string]string {
	return map[string]string{}
}

func needs(t *testing.T, commits ...string) {
	t.Helper()
	if os.Getenv("DMRHUB_RUN_PENDING") != "" {
		return
	}
	var missing []string
	for _, c := range commits {
		if d, ok := pendingCommits()[c]; ok {
			missing = append(missing, "needs "+c+" ("+d+")")
		}
	}
	if len(missing) > 0 {
		t.Skip(strings.Join(missing, "; "))
	}
}

func needRoute() []string { return []string{"hub-a commit 2"} }

func needRows() []string { return []string{"hub-b commit 9", "hub-b commit 10"} }

func needMessages() []string {
	return []string{"hub-b commit 9", "hub-b commit 10", "hub-c commit 14", "hub-c commit 15"}
}

func join(a ...[]string) []string {
	var out []string
	for _, s := range a {
		out = append(out, s...)
	}
	return out
}

type airBurst struct {
	at  float64
	raw layer2.RawBurst
}

func readAirCapture(t *testing.T, path string) []airBurst {
	t.Helper()
	fields := readFields(t, path)
	out := make([]airBurst, 0, len(fields))
	for _, fs := range fields {
		require.Len(t, fs, 2, "%s: %v", path, fs)
		at, err := strconv.ParseFloat(fs[0], 64)
		require.NoError(t, err)
		p, err := hex.DecodeString(fs[1])
		require.NoError(t, err)
		require.Len(t, p, 33)
		out = append(out, airBurst{at: at, raw: layer2.RawBurst(p)})
	}
	return out
}

func readTagged(t *testing.T, path, tag string) [][]byte {
	t.Helper()
	var out [][]byte
	for _, fs := range readFields(t, path) {
		if fs[0] != tag {
			continue
		}
		p, err := hex.DecodeString(fs[len(fs)-1])
		require.NoError(t, err)
		out = append(out, p)
	}
	require.NotEmpty(t, out, "%s has no %s packets", path, tag)
	return out
}

func readFields(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var out [][]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, strings.Fields(line))
	}
	require.NoError(t, sc.Err())
	return out
}

type call struct {
	group    bool
	dst, src uint32
}

type transmission struct {
	call
	slot2  bool
	bursts []layer2.RawBurst
	at     []float64
	frame  []dmrconst.FrameType
	value  []uint
}

func callOf(raw layer2.RawBurst) (call, bool) {
	b := layer2.NewBurst()
	if err := b.DecodeFromBytes(raw); err != nil {
		return call{}, false
	}
	switch d := b.Data.(type) {
	case *pdu.CSBK:
		if p, ok := d.AsPreamble(); ok {
			return call{group: p.Group, dst: p.TargetAddress, src: p.SourceAddress}, true
		}
		if dst, src, ok := d.Addresses(); ok {
			return call{dst: dst, src: src}, true
		}
	case *pdu.DataHeader:
		dst, src, ok := d.Addresses()
		if !ok {
			return call{}, false
		}
		var group bool
		if p, ok := d.AsUnconfirmed(); ok {
			group = p.Group
		} else if p, ok := d.AsConfirmed(); ok {
			group = p.Group
		} else if p, ok := d.AsDefinedData(); ok {
			group = p.Group
		}
		return call{group: group, dst: dst, src: src}, true
	}
	return call{}, false
}

func splitTransmissions(t *testing.T, bursts []airBurst, slot2 bool) []transmission {
	t.Helper()
	var out []transmission
	var cur *transmission
	pending := make([]airBurst, 0, 1)
	last := -1e9
	for _, ab := range bursts {
		if ab.raw.SlotType().DataType == enums.DataTypeIdle {
			continue
		}
		c, ok := callOf(ab.raw)
		gap := ab.at-last > 300
		last = ab.at
		switch {
		case ok && (cur == nil || gap || c != cur.call):
			out = append(out, transmission{call: c, slot2: slot2})
			cur = &out[len(out)-1]
			for _, p := range pending {
				cur.bursts = append(cur.bursts, p.raw)
				cur.at = append(cur.at, p.at)
			}
			pending = nil
		case !ok && (cur == nil || gap):
			cur = nil
			pending = append(pending, ab)
			continue
		}
		cur.bursts = append(cur.bursts, ab.raw)
		cur.at = append(cur.at, ab.at)
	}
	require.Empty(t, pending, "bursts without a call at the end of the capture")
	return out
}

func dataPacket(c call, slot2 bool, stream uint, seq int, raw layer2.RawBurst) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         uint(seq & 0xFF),
		Src:         uint(c.src),
		Dst:         uint(c.dst),
		GroupCall:   c.group,
		Slot:        slot2,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: uint(raw.SlotType().DataType),
		StreamID:    stream,
		DMRData:     raw,
		BER:         -1,
		RSSI:        -1,
	}
}

func (tx *transmission) packets(stream uint) []models.Packet {
	out := make([]models.Packet, 0, len(tx.bursts))
	for i, raw := range tx.bursts {
		p := dataPacket(tx.call, tx.slot2, stream, i, raw)
		if tx.frame != nil {
			p.FrameType, p.DTypeOrVSeq = tx.frame[i], tx.value[i]
		}
		out = append(out, p)
	}
	return out
}

func preambles(t *testing.T, c call, n, follow int) []layer2.RawBurst {
	t.Helper()
	out := make([]layer2.RawBurst, 0, n)
	for i := range n {
		var cs pdu.CSBK
		require.NoError(t, cs.SetPreamble(pdu.PreamblePDU{
			Data:               true,
			Group:              c.group,
			CSBKBlocksToFollow: byte(follow + n - 1 - i),
			TargetAddress:      c.dst,
			SourceAddress:      c.src,
		}))
		cs.LastBlock = true
		out = append(out, csbkBurst(t, &cs))
	}
	return out
}

func csbkBurst(t *testing.T, cs *pdu.CSBK) layer2.RawBurst {
	t.Helper()
	p, err := cs.Encode()
	require.NoError(t, err)
	raw, err := layer2.BuildDataBurst(p, enums.DataTypeCSBK, 7, layer2.SyncSourceMS)
	require.NoError(t, err)
	return raw
}

func packetBursts(t *testing.T, p *packet.Packet) []layer2.RawBurst {
	t.Helper()
	out, err := packet.Bursts(p, 7, layer2.SyncSourceMS)
	require.NoError(t, err)
	return out
}

func payloadOf(raw layer2.RawBurst, voice bool) (enums.DataType, string) {
	if voice {
		s := raw.VoiceSlots()
		return 0, fmt.Sprintf("%x", s)
	}
	dt := raw.SlotType().DataType
	mark := func(p []byte, res fec.Result) string {
		if res.Uncorrectable {
			return hex.EncodeToString(p) + " uncorrectable"
		}
		return hex.EncodeToString(p)
	}
	switch dt {
	case enums.DataTypeRate34:
		p, res := raw.Rate34Payload()
		return dt, mark(p[:], res)
	case enums.DataTypeRate1:
		p, res := raw.Rate1Payload()
		return dt, mark(p[:], res)
	case enums.DataTypePIHeader, enums.DataTypeVoiceLCHeader, enums.DataTypeTerminatorWithLC,
		enums.DataTypeCSBK, enums.DataTypeMBCHeader, enums.DataTypeMBCContinuation,
		enums.DataTypeDataHeader, enums.DataTypeRate12, enums.DataTypeIdle, enums.DataTypeUnifiedSingleBlock:
		p, res := raw.BPTCPayload()
		return dt, mark(p[:], res)
	case enums.DataTypeReserved:
		p := raw.Info()
		return dt, hex.EncodeToString(p[:])
	default:
		p := raw.Info()
		return dt, hex.EncodeToString(p[:])
	}
}

func assertSamePayloads(t *testing.T, want, got []layer2.RawBurst, voice []bool) {
	t.Helper()
	require.Len(t, got, len(want), "bursts")
	for i := range want {
		v := i < len(voice) && voice[i]
		wdt, wp := payloadOf(want[i], v)
		gdt, gp := payloadOf(got[i], v)
		require.Equal(t, wdt, gdt, "burst %d data type", i)
		require.Equal(t, wp, gp, "burst %d payload", i)
	}
}

func assertSamePackets(t *testing.T, want, got []models.Packet, repeater uint, slot2 bool) {
	t.Helper()
	require.Len(t, got, len(want), "packets")
	for i := range want {
		w, g := want[i], got[i]
		require.Equal(t, w.Seq, g.Seq, "packet %d seq", i)
		require.Equal(t, w.Src, g.Src, "packet %d src", i)
		require.Equal(t, w.Dst, g.Dst, "packet %d dst", i)
		require.Equal(t, repeater, g.Repeater, "packet %d repeater", i)
		require.Equal(t, slot2, g.Slot, "packet %d slot", i)
		require.Equal(t, w.GroupCall, g.GroupCall, "packet %d group", i)
		require.Equal(t, w.FrameType, g.FrameType, "packet %d frame type", i)
		require.Equal(t, w.DTypeOrVSeq, g.DTypeOrVSeq, "packet %d data type or position", i)
		require.Equal(t, w.StreamID, g.StreamID, "packet %d stream", i)
		require.Equal(t, hex.EncodeToString(w.DMRData[:]), hex.EncodeToString(g.DMRData[:]), "packet %d burst", i)
	}
}

func burstsOf(pkts []models.Packet) []layer2.RawBurst {
	out := make([]layer2.RawBurst, 0, len(pkts))
	for _, p := range pkts {
		out = append(out, layer2.RawBurst(p.DMRData))
	}
	return out
}

func ofCall(got []models.Packet, src, dst uint) []models.Packet {
	var out []models.Packet
	for _, p := range got {
		if p.Src == src && p.Dst == dst {
			out = append(out, p)
		}
	}
	return out
}

func sendPackets(t *testing.T, send func(models.Packet) error, pkts []models.Packet) {
	t.Helper()
	for _, p := range pkts {
		require.NoError(t, send(p))
		time.Sleep(20 * time.Millisecond)
	}
}

type drainer interface {
	Drain(time.Duration) []models.Packet
}

func drainQuiet(d time.Duration, clients ...drainer) [][]models.Packet {
	out := make([][]models.Packet, len(clients))
	start, deadline := time.Now(), time.Now().Add(d+10*time.Second)
	for {
		got := drainAll(500*time.Millisecond, clients...)
		quiet := true
		for i := range got {
			quiet = quiet && len(got[i]) == 0
			out[i] = append(out[i], got[i]...)
		}
		if (quiet && time.Since(start) >= d) || time.Now().After(deadline) {
			return out
		}
	}
}

func drainAll(d time.Duration, clients ...drainer) [][]models.Packet {
	out := make([][]models.Packet, len(clients))
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = c.Drain(d)
		}()
	}
	wg.Wait()
	return out
}

func keyUp(t *testing.T, c *testutils.MMDVMClient, src, tg uint, slot2 bool, stream uint) {
	t.Helper()
	require.NoError(t, c.SendDMRD(makeGroupVoicePacket(src, tg, stream, slot2)))
	time.Sleep(callHoldTime)
	require.NoError(t, c.SendDMRD(makeGroupVoiceTermPacket(src, tg, stream, slot2)))
	time.Sleep(drainWait)
}

func rowsOf(t *testing.T, db *gorm.DB, table, col string, v any) []map[string]any {
	t.Helper()
	var rows []map[string]any
	require.NoError(t, live(db, table).Where(col+" = ?", v).Find(&rows).Error)
	return rows
}

func live(db *gorm.DB, table string) *gorm.DB {
	q := db.Table(table)
	if table == "calls" {
		q = q.Where("deleted_at IS NULL")
	}
	return q
}

func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(x)
	}
}

func matches(want, got map[string]any) bool {
	for k, w := range want {
		contains, filled := strings.HasSuffix(k, "~"), strings.HasSuffix(k, "+")
		g, ok := got[strings.TrimRight(k, "~+")]
		if !ok {
			return false
		}
		ws, gs := text(w), text(g)
		switch {
		case filled:
			if strings.Trim(gs, "{}[]\" ") == "" {
				return false
			}
		case contains:
			if !strings.Contains(strings.ToLower(gs), strings.ToLower(ws)) {
				return false
			}
		case ws == gs:
		case ws == "true" && (gs == "1" || gs == "t"):
		case ws == "false" && (gs == "0" || gs == "f" || gs == ""):
		default:
			return false
		}
	}
	return true
}

func waitRows(t *testing.T, db *gorm.DB, table, col string, v any, want []map[string]any) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var rows []map[string]any
	for {
		var all []map[string]any
		require.NoError(t, live(db, table).Where(col+" = ?", v).Find(&all).Error, "reading %s", table)
		rows = all
		if len(unmatched(want, rows)) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return rows
}

func unmatched(want, got []map[string]any) []map[string]any {
	used := make([]bool, len(got))
	var out []map[string]any
	for _, w := range want {
		found := false
		for i, g := range got {
			if !used[i] && matches(w, g) {
				used[i], found = true, true
				break
			}
		}
		if !found {
			out = append(out, w)
		}
	}
	return out
}

func keysOf(rows []map[string]any) []string {
	seen := map[string]bool{}
	var keys []string
	for _, r := range rows {
		for k := range r {
			k = strings.TrimRight(k, "~+")
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func project(rows []map[string]any, keys []string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		p := map[string]any{}
		for _, k := range keys {
			if v, ok := r[k]; ok {
				p[k] = text(v)
			}
		}
		out = append(out, p)
	}
	return out
}

func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

type golden struct {
	Source        string           `json:"source"`
	Transmissions []map[string]any `json:"transmissions"`
	Messages      []map[string]any `json:"messages"`
}

func readGolden(t *testing.T, name string) golden {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "golden", name+".json"))
	require.NoError(t, err)
	var g golden
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	require.NoError(t, dec.Decode(&g))
	return g
}

func checkGoldenRows(t *testing.T, db *gorm.DB, name string, srcs, dsts []uint) {
	t.Helper()
	g := readGolden(t, name)
	if *updateGolden {
		writeGolden(t, db, name, g, srcs, dsts)
		return
	}
	t.Run("transmissions", func(t *testing.T) {
		needs(t, needRows()...)
		rows := make([]map[string]any, 0, len(g.Transmissions))
		for _, src := range srcs {
			rows = append(rows, waitRows(t, db, "calls", "source_id", src, g.Transmissions)...)
		}
		if miss := unmatched(g.Transmissions, rows); len(miss) > 0 {
			t.Fatalf("no transmission row for %s; rows: %s", jsonText(miss), jsonText(project(rows, keysOf(g.Transmissions))))
		}
	})
	if len(g.Messages) == 0 {
		return
	}
	t.Run("messages", func(t *testing.T) {
		needs(t, needMessages()...)
		rows := make([]map[string]any, 0, len(g.Messages))
		for _, dst := range dsts {
			rows = append(rows, waitRows(t, db, "messages", "destination_id", dst, g.Messages)...)
		}
		if miss := unmatched(g.Messages, rows); len(miss) > 0 {
			t.Fatalf("no message row for %s; rows: %s", jsonText(miss), jsonText(project(rows, keysOf(g.Messages))))
		}
	})
}

func writeGolden(t *testing.T, db *gorm.DB, name string, g golden, srcs, dsts []uint) {
	t.Helper()
	time.Sleep(5 * time.Second)
	tk, mk := keysOf(g.Transmissions), keysOf(g.Messages)
	tr := make([]map[string]any, 0, len(g.Transmissions))
	var ms []map[string]any
	for _, src := range srcs {
		tr = append(tr, project(rowsOf(t, db, "calls", "source_id", src), tk)...)
	}
	if len(g.Messages) > 0 {
		for _, dst := range dsts {
			ms = append(ms, project(rowsOf(t, db, "messages", "destination_id", dst), mk)...)
		}
	}
	g.Transmissions, g.Messages = tr, ms
	b, err := json.MarshalIndent(g, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join("testdata", "golden", name+".json"), append(b, '\n'), 0o600))
}
