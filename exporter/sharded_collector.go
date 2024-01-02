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
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type shardedCollector struct {
	ctx        context.Context
	base       *baseCollector
	compatible bool
}

// newShardedCollector creates collector collecting metrics about chunks for sharded Mongo.
func newShardedCollector(ctx context.Context, client *mongo.Client, logger *logrus.Logger, compatibleMode bool) *shardedCollector {
	return &shardedCollector{
		ctx:        ctx,
		base:       newBaseCollector(client, logger),
		compatible: compatibleMode,
	}
}

func (d *shardedCollector) Describe(ch chan<- *prometheus.Desc) {
	d.base.Describe(d.ctx, ch, d.collect)
}

func (d *shardedCollector) Collect(ch chan<- prometheus.Metric) {
	d.base.Collect(ch)
}

func (d *shardedCollector) collect(ch chan<- prometheus.Metric) {
	defer measureCollectTime(ch, "mongodb", "sharded")()

	client := d.base.client
	logger := d.base.logger
	prefix := "sharded collection chunks"

	databaseNames, err := client.ListDatabaseNames(d.ctx, bson.D{})
	if err != nil {
		logger.Errorf("cannot get database names: %s", err)
	}
	for _, database := range databaseNames {
		collections := d.getCollectionsForDBName(database)
		for _, row := range collections {
			var ok bool
			if _, ok = row["_id"]; !ok {
				continue
			}
			var rowID string
			if rowID, ok = row["_id"].(string); !ok {
				continue
			}

			chunks := d.getChunksForCollection(row)
			for _, c := range chunks {
				if _, ok = c["dropped"]; !ok {
					continue
				}
				var dropped bool
				if dropped, ok = c["dropped"].(bool); !ok || dropped {
					continue
				}

				labels := make(map[string]string)
				labels["database"] = database
				labels["collection"] = strings.Replace(rowID, fmt.Sprintf("%s.", database), "", 1)

				if _, ok = c["shard"]; !ok {
					continue
				}
				var shard string
				if shard, ok = c["shard"].(string); !ok {
					continue
				}
				labels["shard"] = shard

				logger.Debug("$sharded metrics for config.chunks")
				debugResult(logger, primitive.M{database: c})

				if _, ok = c["nChunks"]; !ok {
					continue
				}
				var chunks int32
				if chunks, ok = c["nChunks"].(int32); !ok {
					continue
				}
				for _, metric := range makeMetrics(prefix, primitive.M{"count": chunks}, labels, d.compatible) {
					ch <- metric
				}
			}
		}
	}
}

func (d *shardedCollector) getCollectionsForDBName(database string) []primitive.M {
	client := d.base.client
	logger := d.base.logger

	cursor := client.Database("config").Collection("collections")
	rs, err := cursor.Find(d.ctx, bson.M{"_id": bson.M{"$regex": fmt.Sprintf("^%s.", database), "$options": "i"}})
	if err != nil {
		logger.Errorf("cannot find _id starting with \"%s.\":%s", database, err)
		return nil
	}

	var decoded []bson.M
	err = rs.All(d.ctx, &decoded)
	if err != nil {
		logger.Errorf("cannot decode collections:%s", err)
		return nil
	}

	return decoded
}

func (d *shardedCollector) getChunksForCollection(row primitive.M) []bson.M {
	if len(row) == 0 {
		return nil
	}

	var chunksMatchPredicate bson.M
	if _, ok := row["timestamp"]; ok {
		if uuid, ok := row["uuid"]; ok {
			chunksMatchPredicate = bson.M{"uuid": uuid}
		}
	} else {
		if id, ok := row["_id"]; ok {
			chunksMatchPredicate = bson.M{"_id": id}
		}
	}

	aggregation := bson.A{
		bson.M{"$match": chunksMatchPredicate},
		bson.M{"$group": bson.M{"_id": "$shard", "cnt": bson.M{"$sum": 1}}},
		bson.M{"$project": bson.M{"_id": 0, "shard": "$_id", "nChunks": "$cnt"}},
		bson.M{"$sort": bson.M{"shard": 1}},
	}

	client := d.base.client
	logger := d.base.logger

	cur, err := client.Database("config").Collection("chunks").Aggregate(context.Background(), aggregation)
	if err != nil {
		logger.Errorf("cannot get $sharded cursor for collection config.chunks: %s", err)
		return nil
	}

	var chunks []bson.M
	err = cur.All(context.Background(), &chunks)
	if err != nil {
		logger.Errorf("cannot decode $sharded for collection config.chunks: %s", err)
		return nil
	}

	return chunks
}

var _ prometheus.Collector = (*shardedCollector)(nil)
