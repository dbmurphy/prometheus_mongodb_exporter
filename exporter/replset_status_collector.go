// mongodb_exporter
// Copyright (C) 2017 Percona LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package exporter

import (
	"context"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	replicationNotEnabled        = 76
	replicationNotYetInitialized = 94
	Unauthorized                 = 13
)

type replSetGetStatusCollector struct {
	ctx  context.Context
	base *baseCollector

	compatibleMode bool
	topologyInfo   labelsGetter
}

// newReplicationSetStatusCollector creates a collector for statistics on replication set.
func newReplicationSetStatusCollector(ctx context.Context, client *mongo.Client, logger *logrus.Logger, compatible bool, topology labelsGetter) *replSetGetStatusCollector {
	return &replSetGetStatusCollector{
		ctx:  ctx,
		base: newBaseCollector(client, logger.WithFields(logrus.Fields{"collector": "replset_status"})),

		compatibleMode: compatible,
		topologyInfo:   topology,
	}
}

func (d *replSetGetStatusCollector) Describe(ch chan<- *prometheus.Desc) {
	d.base.Describe(d.ctx, ch, d.collect)
}

func (d *replSetGetStatusCollector) Collect(ch chan<- prometheus.Metric) {
	d.base.Collect(ch)
}

func (d *replSetGetStatusCollector) collect(ch chan<- prometheus.Metric) {
	defer measureCollectTime(ch, "mongodb", "replset_status")()

	logger := d.base.logger
	client := d.base.client

	cmd := bson.D{{Key: "replSetGetStatus", Value: "1"}}
	res := client.Database("admin").RunCommand(d.ctx, cmd)

	var m bson.M

	if err := res.Decode(&m); err != nil {
		if e, ok := err.(mongo.CommandError); ok {
			if e.Code == replicationNotYetInitialized || e.Code == replicationNotEnabled {
				return
			}
			if e.Code == Unauthorized {
				logger.Errorf("unauthorized to run replSetGetStatus: %s", err)
				os.Exit(1)
			}
		}
		logger.Errorf("cannot get replSetGetStatus: %s", err)

		return
	}

	logger.Debug("replSetGetStatus result:")
	debugResult(logger, m)

	for _, metric := range makeMetrics("", m, d.topologyInfo.baseLabels(), d.compatibleMode) {
		ch <- metric
	}
}

var _ prometheus.Collector = (*replSetGetStatusCollector)(nil)
