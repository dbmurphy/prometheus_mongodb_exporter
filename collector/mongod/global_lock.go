// Copyright 2017 Percona LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mongod

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	//Deprecated: the lockTime sub-field was removed in MongoDB 3.0
	globalLockRatio = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "global_lock",
		Name:      "ratio",
		Help:      "source = serverStatus globalLock.lockTime / globalLock.totalTime",
	})
	globalLockTotalDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, "global_lock", "total"),
		"source = serverStatus globalLock.totalTime",
		nil,
		nil,
	)
)
var (
	globalLockCurrentQueue = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "global_lock_current_queue",
		Help:      "source = serverStatus globalLock.currentQueue",
	}, []string{"type"})
)
var (
	globalLockClient = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "global_lock_client",
		Help:      "source = serverStatus globalLock.activeClients",
	}, []string{"type"})
)

// ClientStats metrics for client stats
type ClientStats struct {
	Total   float64 `bson:"total"`
	Readers float64 `bson:"readers"`
	Writers float64 `bson:"writers"`
}

// Export exports the metrics to prometheus
func (clientStats *ClientStats) Export(ch chan<- prometheus.Metric) {
	globalLockClient.WithLabelValues("reader").Set(clientStats.Readers)
	globalLockClient.WithLabelValues("writer").Set(clientStats.Writers)
}

// QueueStats queue stats
type QueueStats struct {
	Total   float64 `bson:"total"`
	Readers float64 `bson:"readers"`
	Writers float64 `bson:"writers"`
}

// Export exports the metrics to prometheus
func (queueStats *QueueStats) Export(ch chan<- prometheus.Metric) {
	globalLockCurrentQueue.WithLabelValues("reader").Set(queueStats.Readers)
	globalLockCurrentQueue.WithLabelValues("writer").Set(queueStats.Writers)
}

// GlobalLockStats global lock stats
type GlobalLockStats struct {
	TotalTime     float64      `bson:"totalTime"`
	LockTime      float64      `bson:"lockTime"`
	Ratio         float64      `bson:"ratio"`
	CurrentQueue  *QueueStats  `bson:"currentQueue"`
	ActiveClients *ClientStats `bson:"activeClients"`
}

// Export exports the metrics to prometheus
func (globalLock *GlobalLockStats) Export(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(globalLockTotalDesc, prometheus.CounterValue, globalLock.LockTime)

	globalLockRatio.Set(globalLock.Ratio)

	globalLock.CurrentQueue.Export(ch)
	globalLock.ActiveClients.Export(ch)

	globalLockRatio.Collect(ch)
	globalLockCurrentQueue.Collect(ch)
	globalLockClient.Collect(ch)
}

// Describe describes the metrics for prometheus
func (globalLock *GlobalLockStats) Describe(ch chan<- *prometheus.Desc) {
	ch <- globalLockTotalDesc
	globalLockRatio.Describe(ch)
	globalLockCurrentQueue.Describe(ch)
	globalLockClient.Describe(ch)
}
