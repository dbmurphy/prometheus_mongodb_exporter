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

type dbstatsCollector struct {
	ctx  context.Context
	base *baseCollector

	compatibleMode bool
	topologyInfo   labelsGetter

	databaseFilter []string

	freeStorage bool
}

// newDBStatsCollector creates a collector for statistics on database storage.
func newDBStatsCollector(ctx context.Context, client *mongo.Client, logger *logrus.Logger, compatible bool, topology labelsGetter, databaseRegex []string, freeStorage bool) *dbstatsCollector {
	return &dbstatsCollector{
		ctx:  ctx,
		base: newBaseCollector(client, logger.WithFields(logrus.Fields{"collector": "dbstats"})),

		compatibleMode: compatible,
		topologyInfo:   topology,

		databaseFilter: databaseRegex,

		freeStorage: freeStorage,
	}
}

func (d *dbstatsCollector) Describe(ch chan<- *prometheus.Desc) {
	d.base.Describe(d.ctx, ch, d.collect)
}

func (d *dbstatsCollector) Collect(ch chan<- prometheus.Metric) {
	d.base.Collect(ch)
}

func (d *dbstatsCollector) collect(ch chan<- prometheus.Metric) {
	defer measureCollectTime(ch, "mongodb", "dbstats")()

	logger := d.base.logger
	client := d.base.client

	dbNames, err := databases(d.ctx, client, d.databaseFilter, nil)
	if err != nil {
		logger.Errorf("Failed to get database names: %s", err)

		return
	}

	logger.Debugf("getting stats for databases: %v", dbNames)
	for _, db := range dbNames {
		var dbStats bson.M
		var cmd bson.D
		if d.freeStorage {
			cmd = bson.D{{Key: "dbStats", Value: 1}, {Key: "scale", Value: 1}, {Key: "freeStorage", Value: 1}}
		} else {
			cmd = bson.D{{Key: "dbStats", Value: 1}, {Key: "scale", Value: 1}}
		}
		r := client.Database(db).RunCommand(d.ctx, cmd)
		err := r.Decode(&dbStats)
		if err != nil {
			if e, ok := err.(mongo.CommandError); ok {
				if e.Code == Unauthorized {
					logger.Errorf("unauthorized to run replSetGetStatus: %s", err)
					os.Exit(1)
				}
			}
			logger.Errorf("Failed to get $dbstats for database %s: %s", db, err)

			continue
		}

		logger.Debugf("$dbStats metrics for %s", db)
		debugResult(logger, dbStats)

		prefix := "dbstats"

		labels := d.topologyInfo.baseLabels()

		// Since all dbstats will have the same fields, we need to use a label
		// to differentiate metrics between different databases.
		labels["database"] = db

		newMetrics := makeMetrics(prefix, dbStats, labels, d.compatibleMode)
		for _, metric := range newMetrics {
			ch <- metric
		}
	}
}

var _ prometheus.Collector = (*dbstatsCollector)(nil)
