// SPDX-License-Identifier: AGPL-3.0-or-later

package locator

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/lrrp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionEvidenceRequiresMatchingLRRPDocumentAndID(t *testing.T) {
	t.Parallel()
	now := time.Unix(123456789, 0)
	l := newOn(clock.NewFake(now), nil, nil, 5050)
	var seen []*pluginv1.LocationStatus
	l.status = func(s *pluginv1.LocationStatus) error {
		seen = append(seen, s)
		return nil
	}
	key := linkKey{radio: 1234567, service: 5050}
	r := l.radio(key.radio)
	r.req = &request{id: []byte{1, 2}, service: key.service, state: "pending", sentAt: now}
	answer := lrrp.Message{Document: lrrp.DocumentTriggeredAnswer}
	answer.Report.HasRequestID, answer.Report.RequestID = true, []byte{9, 9}
	answer.Report.Success = true
	l.observe(key, r, &answer, false)
	answer.Report.RequestID = r.req.id
	l.observe(linkKey{radio: key.radio, service: 5051}, r, &answer, false)
	assert.Empty(t, seen)
	answer.Report.Success = false
	l.observe(key, r, &answer, false)
	assert.Empty(t, seen, "an answer without a success or result token is inconclusive")
	answer.Report.HasResult, answer.Report.Result = true, lrrp.ResultInvalidCommand
	l.observe(key, r, &answer, false)
	require.Len(t, seen, 1)
	assert.Equal(t, "failed", seen[0].GetState())
	assert.Zero(t, seen[0].GetAcknowledgedAt())
	seen = nil
	answer.Report.HasResult = false
	answer.Report.Success = true
	l.observe(key, r, &answer, false)
	require.Len(t, seen, 1)
	assert.Equal(t, "accepted", seen[0].GetState())
	assert.Equal(t, now.UnixMilli(), seen[0].GetAcknowledgedAt())
	assert.Zero(t, seen[0].GetReportedAt())

	answer.Document = lrrp.DocumentImmediateReport
	l.observe(key, r, &answer, true)
	answer.Document = lrrp.DocumentTriggeredReport
	l.observe(key, r, &answer, false)
	require.Len(t, seen, 1)
	l.observe(key, r, &answer, true)
	require.Len(t, seen, 2)
	assert.Equal(t, "reporting", seen[1].GetState())

	r.req.state = "stopping"
	answer.Document = lrrp.DocumentStopAnswer
	l.observe(key, r, &answer, false)
	require.Len(t, seen, 3)
	assert.Equal(t, "stopped", seen[2].GetState())
	assert.True(t, r.req.suppressesRenewal())
}

func TestManualResendReusesPeriodicIDAndOneShotDoesNotReplaceIt(t *testing.T) {
	t.Parallel()
	now := time.Unix(123456789, 0)
	l := newOn(clock.NewFake(now), func(*pluginv1.Burst) error { return nil }, nil, 5050)
	t.Cleanup(l.Stop)
	key := linkKey{radio: 1234567, service: 5050}
	r := l.radio(key.radio)
	r.tracked, r.heard, r.interval = true, now, time.Minute
	r.req = &request{id: []byte{1, 2}, service: key.service, state: "pending", sentAt: now, interval: r.interval}
	periodic := r.req
	r.manual = &manualRequest{command: &pluginv1.LocationCommand{Radio: key.radio, Id: "resend", Action: "subscribe"}, expires: now.Add(time.Minute)}
	l.runManual(key.radio, r)
	assert.Same(t, periodic, r.manual.req)
	assert.Same(t, periodic, r.req)

	l.links[key].last = now.Add(-settle)
	r.manual = &manualRequest{command: &pluginv1.LocationCommand{Radio: key.radio, Id: "once", Action: "once"}, expires: now.Add(time.Minute)}
	l.runManual(key.radio, r)
	require.NotNil(t, r.manual.req)
	assert.NotEqual(t, periodic.id, r.manual.req.id)
	assert.Same(t, periodic, r.req)
	report := lrrp.Message{Document: lrrp.DocumentImmediateReport}
	report.Report.HasRequestID, report.Report.RequestID = true, r.manual.req.id
	l.observe(key, r, &report, true)
	assert.True(t, r.manual.done)
	assert.Equal(t, "pending", periodic.state)
}
