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
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/percona/mongodb_exporter/internal/tu"
)

func TestReplsetStatusCollector(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client := tu.DefaultTestClient(ctx, t)

	ti := labelsGetterMock{}

	c := newReplicationSetStatusCollector(ctx, client, logrus.New(), false, ti)

	// The last \n at the end of this string is important
	expected := strings.NewReader(`
	# HELP mongodb_rs_myState replSetGetStatus.
	# TYPE mongodb_rs_myState untyped
	mongodb_rs_myState 1
	# HELP mongodb_rs_ok replSetGetStatus.
	# TYPE mongodb_rs_ok untyped
	mongodb_rs_ok 1` + "\n")
	// Filter metrics for 2 reasons:
	// 1. The result is huge
	// 2. We need to check against know values. Don't use metrics that return counters like uptime
	//    or counters like the number of transactions because they won't return a known value to compare
	filter := []string{
		"mongodb_rs_myState",
		"mongodb_rs_ok",
	}
	err := testutil.CollectAndCompare(c, expected, filter...)
	assert.NoError(t, err)
}

func TestReplsetStatusCollectorNoSharding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client := tu.TestClient(ctx, tu.MongoDBStandAlonePort, t)

	ti := labelsGetterMock{}

	c := newReplicationSetStatusCollector(ctx, client, logrus.New(), false, ti)

	// Replication set metrics should not be generated for unsharded server
	count := testutil.CollectAndCount(c)

	metaMetricCount := 1
	assert.Equal(t, metaMetricCount, count, "Mismatch in metric count for collector run on unsharded server")
}
