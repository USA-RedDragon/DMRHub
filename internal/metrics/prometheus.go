// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	// KV Store metrics
	KVOperationsTotal   *prometheus.CounterVec
	KVOperationDuration *prometheus.HistogramVec
	KVKeysTotal         prometheus.Gauge
	KVExpiredKeysTotal  prometheus.Counter
	KVCleanupDuration   prometheus.Histogram
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
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
	}
	metrics.register()
	return metrics
}

func (m *Metrics) register() {
	prometheus.MustRegister(m.KVOperationsTotal)
	prometheus.MustRegister(m.KVOperationDuration)
	prometheus.MustRegister(m.KVKeysTotal)
	prometheus.MustRegister(m.KVExpiredKeysTotal)
	prometheus.MustRegister(m.KVCleanupDuration)
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
