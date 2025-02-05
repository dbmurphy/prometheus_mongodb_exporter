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
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/percona/mongodb_exporter/internal/tu"
)

func TestCollStatsCollector(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client := tu.DefaultTestClient(ctx, t)

	database := client.Database("testdb")
	database.Drop(ctx) //nolint

	defer func() {
		err := database.Drop(ctx)
		assert.NoError(t, err)
	}()

	for i := 0; i < 3; i++ {
		coll := fmt.Sprintf("testcol_%02d", i)
		_, err := database.Collection(coll).InsertOne(ctx, bson.M{"f1": 1, "f2": "2"})
		assert.NoError(t, err)
	}

	ti := labelsGetterMock{}

	collection := []string{"testdb.testcol_00", "testdb.testcol_01", "testdb.testcol_02"}
	logger := logrus.New()
	c := newCollectionStatsCollector(ctx, client, logger, false, ti, collection, false)

	// The last \n at the end of this string is important
	expected := strings.NewReader(`
# HELP mongodb_collstats_latencyStats_commands_latency collstats.latencyStats.commands.latency
# TYPE mongodb_collstats_latencyStats_commands_latency untyped
mongodb_collstats_latencyStats_commands_latency{collection="testcol_00",database="testdb"} 0
mongodb_collstats_latencyStats_commands_latency{collection="testcol_01",database="testdb"} 0
mongodb_collstats_latencyStats_commands_latency{collection="testcol_02",database="testdb"} 0
# HELP mongodb_collstats_latencyStats_transactions_ops collstats.latencyStats.transactions.ops
# TYPE mongodb_collstats_latencyStats_transactions_ops untyped
mongodb_collstats_latencyStats_transactions_ops{collection="testcol_00",database="testdb"} 0
mongodb_collstats_latencyStats_transactions_ops{collection="testcol_01",database="testdb"} 0
mongodb_collstats_latencyStats_transactions_ops{collection="testcol_02",database="testdb"} 0
# HELP mongodb_collstats_storageStats_indexSizes collstats.storageStats.indexSizes
# TYPE mongodb_collstats_storageStats_indexSizes untyped
mongodb_collstats_storageStats_indexSizes{collection="testcol_00",database="testdb",index_name="_id_"} 4096
mongodb_collstats_storageStats_indexSizes{collection="testcol_01",database="testdb",index_name="_id_"} 4096
mongodb_collstats_storageStats_indexSizes{collection="testcol_02",database="testdb",index_name="_id_"} 4096
# HELP mongodb_collstats_storageStats_capped collstats.storageStats.capped
# TYPE mongodb_collstats_storageStats_capped untyped
mongodb_collstats_storageStats_capped{collection="testcol_00",database="testdb"} 0
mongodb_collstats_storageStats_capped{collection="testcol_01",database="testdb"} 0
mongodb_collstats_storageStats_capped{collection="testcol_02",database="testdb"} 0` +
		"\n")

	// Filter metrics for 2 reasons:
	// 1. The result is huge
	// 2. We need to check against know values. Don't use metrics that return counters like uptime
	//    or counters like the number of transactions because they won't return a known value to compare
	filter := []string{
		"mongodb_collstats_latencyStats_commands_latency",
		"mongodb_collstats_storageStats_capped",
		"mongodb_collstats_storageStats_indexSizes",
		"mongodb_collstats_latencyStats_transactions_ops",
	}
	err := testutil.CollectAndCompare(c, expected, filter...)
	assert.NoError(t, err)
}
