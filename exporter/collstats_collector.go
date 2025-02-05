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
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type collstatsCollector struct {
	ctx  context.Context
	base *baseCollector

	compatibleMode  bool
	discoveringMode bool
	enableDetails   bool
	topologyInfo    labelsGetter

	collections []string
}

// newCollectionStatsCollector creates a collector for statistics about collections.
func newCollectionStatsCollector(ctx context.Context, client *mongo.Client, logger *logrus.Logger, discovery bool, topology labelsGetter, collections []string, enableDetails bool) *collstatsCollector {
	return &collstatsCollector{
		ctx:  ctx,
		base: newBaseCollector(client, logger.WithFields(logrus.Fields{"collector": "collstats"})),

		compatibleMode:  false, // there are no compatible metrics for this collector.
		discoveringMode: discovery,
		topologyInfo:    topology,

		collections:   collections,
		enableDetails: enableDetails,
	}
}

func (d *collstatsCollector) Describe(ch chan<- *prometheus.Desc) {
	d.base.Describe(d.ctx, ch, d.collect)
}

func (d *collstatsCollector) Collect(ch chan<- prometheus.Metric) {
	d.base.Collect(ch)
}

func (d *collstatsCollector) collect(ch chan<- prometheus.Metric) {
	defer measureCollectTime(ch, "mongodb", "collstats")()

	client := d.base.client
	logger := d.base.logger

	var collections []string
	if d.discoveringMode {
		onlyCollectionsNamespaces, err := listAllCollections(d.ctx, client, d.collections, systemDBs, true)
		if err != nil {
			logger.Errorf("cannot auto discover databases and collections: %s", err.Error())

			return
		}

		collections = fromMapToSlice(onlyCollectionsNamespaces)
	} else {
		var err error
		collections, err = checkNamespacesForViews(d.ctx, client, d.collections)
		if err != nil {
			logger.Errorf("cannot list collections: %s", err.Error())
			return
		}
	}

	for _, dbCollection := range collections {
		parts := strings.Split(dbCollection, ".")
		if len(parts) < 2 { //nolint:gomnd
			continue
		}

		database := parts[0]
		collection := strings.Join(parts[1:], ".") // support collections having a .

		// exclude system collections
		if strings.HasPrefix(collection, "system.") {
			continue
		}

		aggregation := bson.D{
			{
				Key: "$collStats",
				Value: bson.M{
					// TODO: PMM-9568 : Add support to handle histogram metrics
					"latencyStats": bson.M{"histograms": false},
					"storageStats": bson.M{"scale": 1},
				},
			},
		}

		pipeline := mongo.Pipeline{aggregation}

		if !d.enableDetails {
			project := bson.D{
				{
					Key: "$project", Value: bson.M{
						"storageStats.wiredTiger":   0,
						"storageStats.indexDetails": 0,
					},
				},
			}
			pipeline = append(pipeline, project)
		}

		cursor, err := client.Database(database).Collection(collection).Aggregate(d.ctx, pipeline)
		if err != nil {
			logger.Errorf("cannot get $collstats cursor for collection %s.%s: %s", database, collection, err)

			continue
		}

		var stats []bson.M
		if err = cursor.All(d.ctx, &stats); err != nil {
			logger.Errorf("cannot get $collstats for collection %s.%s: %s", database, collection, err)

			continue
		}

		logger.Debugf("$collStats metrics for %s.%s", database, collection)
		debugResult(logger, stats)

		prefix := "collstats"
		labels := d.topologyInfo.baseLabels()
		labels["database"] = database
		labels["collection"] = collection

		for _, metrics := range stats {
			if shard, ok := metrics["shard"].(string); ok {
				labels["shard"] = shard
			}

			for _, metric := range makeMetrics(prefix, metrics, labels, d.compatibleMode) {
				ch <- metric
			}
		}
	}
}

var _ prometheus.Collector = (*collstatsCollector)(nil)
