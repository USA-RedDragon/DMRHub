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

package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	// KV Store metrics
	KVOperationsTotal   *prometheus.CounterVec
	KVOperationDuration *prometheus.HistogramVec
	KVKeysTotal         prometheus.Gauge
	KVExpiredKeysTotal  prometheus.Counter
	KVCleanupDuration   prometheus.Histogram

	// UndeliverableBurstsTotal counts bursts that a target could not carry.
	UndeliverableBurstsTotal *prometheus.CounterVec

	// IngressDroppedBurstsTotal counts bursts that the hub took in and did
	// not route, by reason.
	IngressDroppedBurstsTotal *prometheus.CounterVec

	// TrackerDroppedTotal counts packets that the call tracker dropped.
	TrackerDroppedTotal prometheus.Counter

	// MessagesDroppedTotal counts data messages that the message store dropped.
	MessagesDroppedTotal prometheus.Counter
}

// DropReasonStreamFromOtherSource is the IngressDroppedBurstsTotal reason
// for a burst of a stream that another source is already sending.
const DropReasonStreamFromOtherSource = "stream_from_other_source"

var (
	metricsOnce     sync.Once //nolint:gochecknoglobals
	metricsInstance *Metrics  //nolint:gochecknoglobals
)

// NewMetrics returns a singleton Metrics instance. Prometheus collectors
// are registered on the default registry exactly once, so it is safe to
// call NewMetrics from multiple goroutines or test cases.
func NewMetrics() *Metrics {
	metricsOnce.Do(func() {
		metricsInstance = &Metrics{
			KVOperationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "kv_operations_total",
				Help: "The total number of KV operations performed",
			}, []string{"operation", "status"}),
			KVOperationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "kv_operation_duration_seconds",
				Help:    "Duration of KV operations",
				Buckets: prometheus.DefBuckets,
			}, []string{"operation"}),
			KVKeysTotal: prometheus.NewGauge(prometheus.GaugeOpts{
				Name: "kv_keys_total",
				Help: "The current number of keys in the KV store",
			}),
			KVExpiredKeysTotal: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "kv_expired_keys_total",
				Help: "The total number of expired keys cleaned up",
			}),
			KVCleanupDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
				Name:    "kv_cleanup_duration_seconds",
				Help:    "Duration of KV cleanup operations",
				Buckets: prometheus.DefBuckets,
			}),
			UndeliverableBurstsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "dmr_undeliverable_bursts_total",
				Help: "The total number of bursts that a target could not carry",
			}, []string{"target"}),
			IngressDroppedBurstsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "dmr_ingress_dropped_bursts_total",
				Help: "The total number of incoming bursts that were not routed, by reason",
			}, []string{"reason"}),
			TrackerDroppedTotal: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "call_tracker_dropped_packets_total",
				Help: "The total number of packets that the call tracker did not record because its queue was full",
			}),
			MessagesDroppedTotal: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "message_store_dropped_total",
				Help: "The total number of data messages that the message store did not save because its queue was full",
			}),
		}
		metricsInstance.register()
	})
	return metricsInstance
}

func (m *Metrics) register() {
	prometheus.MustRegister(m.KVOperationsTotal)
	prometheus.MustRegister(m.KVOperationDuration)
	prometheus.MustRegister(m.KVKeysTotal)
	prometheus.MustRegister(m.KVExpiredKeysTotal)
	prometheus.MustRegister(m.KVCleanupDuration)
	prometheus.MustRegister(m.UndeliverableBurstsTotal)
	prometheus.MustRegister(m.IngressDroppedBurstsTotal)
	prometheus.MustRegister(m.TrackerDroppedTotal)
	prometheus.MustRegister(m.MessagesDroppedTotal)
}

// KV Store metrics methods
func (m *Metrics) RecordKVOperation(operation, status string, duration float64) {
	m.KVOperationsTotal.WithLabelValues(operation, status).Inc()
	m.KVOperationDuration.WithLabelValues(operation).Observe(duration)
}

func (m *Metrics) SetKVKeysTotal(count float64) {
	m.KVKeysTotal.Set(count)
}

func (m *Metrics) IncrementKVExpiredKeys(count float64) {
	m.KVExpiredKeysTotal.Add(count)
}

func (m *Metrics) RecordKVCleanup(duration float64) {
	m.KVCleanupDuration.Observe(duration)
}

// IncrementUndeliverable counts a burst that target could not carry.
func (m *Metrics) IncrementUndeliverable(target string) {
	m.UndeliverableBurstsTotal.WithLabelValues(target).Inc()
}

// IncrementIngressDropped counts an incoming burst that was not routed.
func (m *Metrics) IncrementIngressDropped(reason string) {
	m.IngressDroppedBurstsTotal.WithLabelValues(reason).Inc()
}

// IncrementTrackerDropped counts a packet that the call tracker dropped.
func (m *Metrics) IncrementTrackerDropped() {
	m.TrackerDroppedTotal.Inc()
}

// IncrementMessagesDropped counts a data message that the message store
// dropped.
func (m *Metrics) IncrementMessagesDropped() {
	m.MessagesDroppedTotal.Inc()
}
