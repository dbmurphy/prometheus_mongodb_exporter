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

const (
	dbName = "testdb"
)

func TestDBStatsCollector(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client := tu.DefaultTestClient(ctx, t)

	database := client.Database(dbName)
	database.Drop(ctx) //nolint

	defer func() {
		err := database.Drop(ctx)
		assert.NoError(t, err)
	}()

	for i := 0; i < 3; i++ {
		coll := fmt.Sprintf("testcol_%02d", i)
		for j := 0; j < 10; j++ {
			_, err := database.Collection(coll).InsertOne(ctx, bson.M{"f1": j, "f2": "2"})
			assert.NoError(t, err)
		}
	}

	ti := labelsGetterMock{}

	c := newDBStatsCollector(ctx, client, logrus.New(), false, ti, []string{dbName})
	expected := strings.NewReader(`
	# HELP mongodb_dbstats_collections dbstats.
	# TYPE mongodb_dbstats_collections untyped
	mongodb_dbstats_collections{database="testdb"} 3
	# HELP mongodb_dbstats_indexes dbstats.
	# TYPE mongodb_dbstats_indexes untyped
	mongodb_dbstats_indexes{database="testdb"} 3
	# HELP mongodb_dbstats_objects dbstats.
	# TYPE mongodb_dbstats_objects untyped
	mongodb_dbstats_objects{database="testdb"} 30` + "\n")

	// Only look at metrics created by our activity
	filters := []string{
		"mongodb_dbstats_collections",
		"mongodb_dbstats_indexes",
		"mongodb_dbstats_objects",
	}
	err := testutil.CollectAndCompare(c, expected, filters...)
	assert.NoError(t, err)
}
