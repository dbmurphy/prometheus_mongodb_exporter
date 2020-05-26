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

package mongos

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	shardingChangelogInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "sharding",
		Name:      "changelog_10min_total",
		Help:      "Total # of Cluster Balancer log events over the last 10 minutes",
	}, []string{"event"})
)

type ShardingChangelogSummaryId struct {
	Event string `bson:"event"`
	Note  string `bson:"note"`
}

type ShardingChangelogSummary struct {
	Id    *ShardingChangelogSummaryId `bson:"_id"`
	Count float64                     `bson:"count"`
}

type ShardingChangelogStats struct {
	Items *[]ShardingChangelogSummary
}

func (status *ShardingChangelogStats) Export(ch chan<- prometheus.Metric) {
	// set all expected event types to zero first, so they show in results if there was no events in the current time period
	shardingChangelogInfo.With(prometheus.Labels{"event": "moveChunk.start"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "moveChunk.to"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "moveChunk.to_failed"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "moveChunk.from"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "moveChunk.from_failed"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "moveChunk.commit"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "addShard"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "removeShard.start"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "shardCollection"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "shardCollection.start"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "split"}).Set(0)
	shardingChangelogInfo.With(prometheus.Labels{"event": "multi-split"}).Set(0)

	// set counts for events found in our query
	for _, item := range *status.Items {
		event := item.Id.Event
		note := item.Id.Note
		count := item.Count
		switch event {
		case "moveChunk.to":
			if note == "success" || note == "" {
				ls := prometheus.Labels{"event": event}
				shardingChangelogInfo.With(ls).Set(count)
			} else {
				ls := prometheus.Labels{"event": event + "_failed"}
				shardingChangelogInfo.With(ls).Set(count)
			}
		case "moveChunk.from":
			if note == "success" || note == "" {
				ls := prometheus.Labels{"event": event}
				shardingChangelogInfo.With(ls).Set(count)
			} else {
				ls := prometheus.Labels{"event": event + "_failed"}
				shardingChangelogInfo.With(ls).Set(count)
			}
		default:
			ls := prometheus.Labels{"event": event}
			shardingChangelogInfo.With(ls).Set(count)
		}
	}
	shardingChangelogInfo.Collect(ch)
}

func (status *ShardingChangelogStats) Describe(ch chan<- *prometheus.Desc) {
	shardingChangelogInfo.Describe(ch)
}

// GetShardingChangelogStatus gets sharding changelog status.
func GetShardingChangelogStatus(client *mongo.Client) *ShardingChangelogStats {
	var qresults []ShardingChangelogSummary
	coll := client.Database("config").Collection("changelog")
	match := bson.M{"time": bson.M{"$gt": time.Now().Add(-10 * time.Minute)}}
	group := bson.M{"_id": bson.M{"event": "$what", "note": "$details.note"}, "count": bson.M{"$sum": 1}}

	c, err := coll.Aggregate(context.TODO(), []bson.M{{"$match": match}, {"$group": group}})
	if err != nil {
		log.Errorf("Failed to aggregate sharding changelog events: %s.", err)
	}

	defer c.Close(context.TODO())

	for c.Next(context.TODO()) {
		s := &ShardingChangelogSummary{}
		if err := c.Decode(s); err != nil {
			log.Error(err)
			continue
		}
		qresults = append(qresults, *s)
	}

	if err := c.Err(); err != nil {
		log.Error(err)
	}

	results := &ShardingChangelogStats{}
	results.Items = &qresults
	return results
}
