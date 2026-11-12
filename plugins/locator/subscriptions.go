// SPDX-License-Identifier: AGPL-3.0-or-later

package locator

import (
	"bytes"
	"encoding/binary"
	"log/slog"
	"time"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/lrrp"
)

const (
	manualRouteAge = 2 * time.Minute
	answerWait     = time.Minute
)

type manualRequest struct {
	command *pluginv1.LocationCommand
	req     *request
	expires time.Time
	done    bool
}

func (m *manualRequest) periodicInFlight() bool {
	return m != nil && !m.done && m.command.GetAction() != "once"
}

func (l *Locator) restartTracking(k linkKey, ln *link, r *radio) {
	q := r.req
	q.state, q.detail = "pending", ""
	q.ack, q.reported, r.lastPos = time.Time{}, time.Time{}, time.Time{}
	l.resendTracking(k, ln, q)
}

func (l *Locator) resendTracking(k linkKey, ln *link, q *request) {
	q.sentAt, q.slot, q.cc = l.clock.Now(), ln.slot, ln.cc
	if q.state == "failed" {
		q.state, q.detail = "pending", ""
	}
	l.publishPeriodic(k.radio, q)
	l.sendLocationRequest(k, ln, q, true)
}

func (l *Locator) publish(s *pluginv1.LocationStatus) {
	if l.status != nil {
		if err := l.status(s); err != nil {
			slog.Warn("Locator: cannot send LRRP status", "radio", s.GetRadio(), "error", err)
		}
	}
}

func (l *Locator) publishPeriodic(id uint32, q *request) {
	l.publish(&pluginv1.LocationStatus{
		Radio: id, RequestId: q.id, Service: q.service, Slot: q.slot, ColorCode: uint32(q.cc),
		IntervalSeconds: uint32(q.interval / time.Second), //nolint:gosec // configured interval
		State:           q.state, Detail: q.detail, SentAt: millis(q.sentAt),
		AcknowledgedAt: millis(q.ack), ReportedAt: millis(q.reported),
	})
}

func millis(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UnixMilli()
}

func locationTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func (l *Locator) restore(s *pluginv1.LocationStatus) {
	l.mu.Lock()
	defer l.unlock()
	r := l.radio(s.GetRadio())
	if r.req != nil || len(s.GetRequestId()) == 0 {
		return
	}
	r.req = &request{
		id: s.GetRequestId(), service: s.GetService(), slot: s.GetSlot(), cc: enums.ColorCode(s.GetColorCode() & 0xf), //nolint:gosec // masked to 4 bits
		interval: time.Duration(s.GetIntervalSeconds()) * time.Second, state: s.GetState(), detail: s.GetDetail(),
		sentAt: locationTime(s.GetSentAt()), ack: locationTime(s.GetAcknowledgedAt()), reported: locationTime(s.GetReportedAt()),
	}
	r.lastPos = r.req.reported
}

func (l *Locator) command(c *pluginv1.LocationCommand) {
	l.mu.Lock()
	defer l.unlock()
	r := l.radio(c.GetRadio())
	if !r.tracked || (r.manual != nil && r.manual.command.GetId() == c.GetId()) {
		return
	}
	r.manual = &manualRequest{command: c, expires: locationTime(c.GetExpiresAt())}
}

func (l *Locator) manualStatus(m *manualRequest, state, detail string) {
	if state != "pending" {
		m.done = true
	}
	s := &pluginv1.LocationStatus{Radio: m.command.GetRadio(), CommandId: m.command.GetId(), State: state, Detail: detail}
	if m.req != nil {
		s.RequestId, s.Service, s.SentAt = m.req.id, m.req.service, millis(m.req.sentAt)
	}
	l.publish(s)
}

func (q *request) suppressesRenewal() bool {
	return q != nil && (q.state == "stopping" || q.state == "stopped" || q.state == "stop_failed")
}

func (l *Locator) runManual(id uint32, r *radio) {
	m := r.manual
	if m == nil || m.done {
		return
	}
	if !l.clock.Now().Before(m.expires) {
		if m.req == nil {
			l.manualStatus(m, "failed", "No recent route to the radio")
		} else {
			l.manualStatus(m, "failed", "No LRRP answer received")
			if m.command.GetAction() == "unsubscribe" && r.req == m.req && r.req.state == "stopping" {
				r.req.state, r.req.detail = "stop_failed", "No LRRP stop answer received"
				l.publishPeriodic(id, r.req)
			}
		}
		return
	}
	if m.req != nil || !r.tracked || r.heard.IsZero() || l.clock.Since(r.heard) >= manualRouteAge || l.busy(id) {
		return
	}
	action := m.command.GetAction()
	if action == "unsubscribe" && (r.req == nil || r.req.state == "stopped") {
		l.manualStatus(m, "failed", "No known periodic request to stop")
		return
	}
	service := l.service
	if r.req != nil {
		service = r.req.service
	} else if r.service != 0 {
		service = r.service
	}
	k := linkKey{radio: id, service: service}
	ln := l.links[k]
	if ln == nil {
		var err error
		ln, err = newLink(service, l.clock.Now())
		if err != nil {
			l.manualStatus(m, "failed", "Could not open radio link")
			return
		}
		l.links[k] = ln
	}
	ln.slot, ln.cc, ln.last = r.slot, r.cc, l.clock.Now()
	switch action {
	case "subscribe":
		if r.req == nil || r.req.state == "stopped" {
			l.startTracking(k, ln, r)
		} else if r.req.suppressesRenewal() || r.req.interval != r.interval {
			l.sendStop(k, ln, r.req.id)
			r.req.interval = r.interval
			l.restartTracking(k, ln, r)
		} else {
			// Reuse the on-air ID. A duplicate response can be reported without
			// risking a second active periodic request.
			l.resendTracking(k, ln, r.req)
		}
		m.req = r.req
		r.retried = true
	case "once":
		m.req = l.newRequest(k, ln, 0)
		l.sendLocationRequest(k, ln, m.req, false)
	case "unsubscribe":
		m.req = r.req
		m.req.state, m.req.detail = "stopping", ""
		m.req.sentAt = l.clock.Now()
		l.publishPeriodic(id, m.req)
		l.sendStop(k, ln, m.req.id)
	default:
		l.manualStatus(m, "failed", "Unknown LRRP operation")
		return
	}
	m.expires = l.clock.Now().Add(answerWait)
	l.manualStatus(m, "pending", "")
}

func (l *Locator) newRequest(k linkKey, ln *link, interval time.Duration) *request {
	l.reqID++
	var id [4]byte
	binary.BigEndian.PutUint32(id[:], l.reqID)
	return &request{id: id[:], service: k.service, slot: ln.slot, cc: ln.cc, interval: interval, sentAt: l.clock.Now(), state: "pending"}
}

func (l *Locator) sendLocationRequest(k linkKey, ln *link, q *request, periodic bool) {
	doc := lrrp.DocumentImmediateRequest
	if periodic {
		doc = lrrp.DocumentTriggeredRequest
	}
	m := lrrp.Message{Document: doc}
	m.Request = lrrp.Request{
		RequestID: q.id, HasRequestID: true, Periodic: periodic, HasInterval: periodic,
		Interval:        uint32(q.interval / time.Second), //nolint:gosec // configured interval
		AccuracyAndTime: true, Speed: true, Direction: true,
	}
	buf, err := m.AppendBinary(nil)
	if err != nil {
		slog.Error("Locator: cannot encode LRRP request", "error", err)
		return
	}
	l.queue(k, ln, ip.PortLRRP, buf)
}

func lrrpResult(report *lrrp.Report) (string, string) {
	switch {
	case report.HasError:
		return "failed", report.Error.Code.String()
	case report.HasResult && report.Result != lrrp.ResultSuccess:
		return "failed", report.Result.String()
	case report.Success || report.HasResult:
		return "accepted", ""
	default:
		return "", ""
	}
}

func requestMatches(q *request, k linkKey, id []byte) bool {
	return q != nil && q.service == k.service && len(id) != 0 && bytes.Equal(q.id, id)
}

func (l *Locator) observe(k linkKey, r *radio, m *lrrp.Message, validFix bool) {
	id := m.Report.RequestID
	if m.Report.HasShortRequestID && !m.Report.HasRequestID {
		id = []byte{m.Report.ShortRequestID}
	}
	state, detail := lrrpResult(&m.Report)
	now := l.clock.Now()
	q := r.req
	matching := requestMatches(q, k, id)
	switch m.Document {
	case lrrp.DocumentTriggeredAnswer:
		if matching && !q.suppressesRenewal() && state != "" {
			if state == "accepted" {
				q.ack, q.detail = now, ""
				if q.reported.IsZero() {
					q.state = state
				}
			} else if q.ack.IsZero() && q.reported.IsZero() {
				q.state, q.detail = state, detail
			}
			l.publishPeriodic(k.radio, q)
		}
	case lrrp.DocumentTriggeredReport:
		if matching && validFix {
			q.reported = now
			if !q.suppressesRenewal() {
				q.state, q.detail = "reporting", ""
				r.lastPos, r.retried = now, false
			}
			l.publishPeriodic(k.radio, q)
		}
	case lrrp.DocumentStopAnswer:
		if matching && q.suppressesRenewal() && state != "" {
			q.state, q.detail = "stop_failed", detail
			if state == "accepted" {
				q.state, q.detail = "stopped", ""
			}
			l.publishPeriodic(k.radio, q)
		}
	}
	job := r.manual
	if job == nil || job.done || !requestMatches(job.req, k, id) {
		return
	}
	switch job.command.GetAction() {
	case "subscribe":
		if m.Document == lrrp.DocumentTriggeredAnswer && state != "" {
			l.manualStatus(job, state, detail)
		}
	case "once":
		if m.Document == lrrp.DocumentImmediateReport {
			if validFix {
				l.manualStatus(job, "reported", "")
			} else {
				if detail == "" {
					detail = "Radio did not return a valid position"
				}
				l.manualStatus(job, "failed", detail)
			}
		}
	case "unsubscribe":
		if m.Document == lrrp.DocumentStopAnswer && state != "" {
			if state == "accepted" {
				state = "stopped"
			}
			l.manualStatus(job, state, detail)
		}
	}
}
