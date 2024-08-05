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
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/percona/mongodb_exporter/internal/tu"
)

func TestServerStatusDataCollector(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client := tu.DefaultTestClient(ctx, t)

	ti := labelsGetterMock{}

	c := newServerStatusCollector(ctx, client, logrus.New(), false, ti)

	// The last \n at the end of this string is important
	expected := strings.NewReader(`
	# HELP mongodb_ss_mem_bits serverStatus.mem.
	# TYPE mongodb_ss_mem_bits untyped
	mongodb_ss_mem_bits 64
	# HELP mongodb_ss_metrics_commands_connPoolSync_failed serverStatus.metrics.commands.connPoolSync.
	# TYPE mongodb_ss_metrics_commands_connPoolSync_failed untyped
	mongodb_ss_metrics_commands_connPoolSync_failed 0` + "\n")
	// Filter metrics for 2 reasons:
	// 1. The result is huge
	// 2. We need to check against know values. Don't use metrics that return counters like uptime
	//    or counters like the number of transactions because they won't return a known value to compare
	filter := []string{
		"mongodb_ss_mem_bits",
		"mongodb_ss_metrics_commands_connPoolSync_failed",
	}
	err := testutil.CollectAndCompare(c, expected, filter...)
	assert.NoError(t, err)
}
