// mongodb_exporter
// Copyright (C) 2017 Percona LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package exporter

import (
	"context"
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type diagnosticDataCollector struct {
	ctx    context.Context
	client *mongo.Client
	logger *logrus.Logger

	lock         sync.Mutex
	metricsCache []prometheus.Metric

	compatibleMode bool
	topologyInfo   labelsGetter
}

func NewDiagnosticDataCollector(ctx context.Context, client *mongo.Client, logger *logrus.Logger, compatible bool, topology labelsGetter) *diagnosticDataCollector {
	return &diagnosticDataCollector{
		ctx:            ctx,
		client:         client,
		logger:         logger,
		compatibleMode: compatible,
		topologyInfo:   topology,
	}
}

func (d *diagnosticDataCollector) Describe(ch chan<- *prometheus.Desc) {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.metricsCache = make([]prometheus.Metric, 0, defaultCacheSize)

	// This is a copy/paste of prometheus.DescribeByCollect(d, ch) with the aggreated functionality
	// to populate the metrics cache. Since on each scrape Prometheus will call Describe and inmediatelly
	// after it will call Collect, it is safe to populate the cache here.
	metrics := make(chan prometheus.Metric)
	go func() {
		d.collect(metrics)
		close(metrics)
	}()

	for m := range metrics {
		d.metricsCache = append(d.metricsCache, m) // populate the cache
		ch <- m.Desc()
	}
}

func (d *diagnosticDataCollector) Collect(ch chan<- prometheus.Metric) {
	d.lock.Lock()
	defer d.lock.Unlock()

	for _, metric := range d.metricsCache {
		ch <- metric
	}
}

func (d *diagnosticDataCollector) collect(ch chan<- prometheus.Metric) {
	var m bson.M

	cmd := bson.D{{Key: "getDiagnosticData", Value: "1"}}
	res := d.client.Database("admin").RunCommand(d.ctx, cmd)
	if res.Err() != nil {
		if isArbiter, _ := isArbiter(d.ctx, d.client); isArbiter {
			return
		}
	}

	if err := res.Decode(&m); err != nil {
		d.logger.Errorf("cannot run getDiagnosticData: %s", err)
	}

	if m == nil || m["data"] == nil {
		d.logger.Error("cannot run getDiagnosticData: response is empty")
	}

	m, ok := m["data"].(bson.M)
	if !ok {
		err := errors.Wrapf(errUnexpectedDataType, "%T for data field", m["data"])
		d.logger.Errorf("cannot decode getDiagnosticData: %s", err)
	}

	d.logger.Debug("getDiagnosticData result")
	debugResult(d.logger, m)

	metrics := makeMetrics("", m, d.topologyInfo.baseLabels(), d.compatibleMode)
	metrics = append(metrics, locksMetrics(m)...)

	if d.compatibleMode {
		metrics = append(metrics, specialMetrics(d.ctx, d.client, m, d.logger)...)

		if cem, err := cacheEvictedTotalMetric(m); err == nil {
			metrics = append(metrics, cem)
		}

		nodeType, err := getNodeType(d.ctx, d.client)
		if err != nil {
			d.logger.Errorf("Cannot get node type to check if this is a mongos: %s", err)
		} else if nodeType == typeMongos {
			metrics = append(metrics, mongosMetrics(d.ctx, d.client, d.logger)...)
		}
	}

	for _, metric := range metrics {
		ch <- metric
	}
}

// check interface.
var _ prometheus.Collector = (*diagnosticDataCollector)(nil)
