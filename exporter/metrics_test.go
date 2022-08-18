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
	"testing"
	"time"

	"github.com/AlekSi/pointer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Test metric renaming and labeling.
func TestMetricName(t *testing.T) {
	tcs := []struct {
		prefix     string
		name       string
		wantMetric string
		wantLabel  string
	}{
		{
			prefix:     "serverStatus.metrics.commands.saslStart.",
			name:       "total",
			wantMetric: "mongodb_ss_metrics_commands_saslStart_total",
		},
		{
			prefix:     "serverStatus.metrics.commands._configsvrShardCollection.",
			name:       "failed",
			wantMetric: "mongodb_ss_metrics_commands_configsvrShardCollection_failed",
		},
		{
			prefix:     "serverStatus.wiredTiger.lock.",
			name:       "metadata lock acquisitions",
			wantMetric: "mongodb_ss_wt_lock_metadata_lock_acquisitions",
		},
		{
			prefix:     "serverStatus.wiredTiger.perf.",
			name:       "file system write latency histogram (bucket 5) - 500-999ms",
			wantMetric: "mongodb_ss_wt_perf",
			wantLabel:  "perf_bucket",
		},
		{
			prefix:     "serverStatus.wiredTiger.transaction.",
			name:       "rollback to stable updates removed from lookaside",
			wantMetric: "mongodb_ss_wt_txn_rollback_to_stable_updates_removed_from_lookaside",
		},
	}

	for _, tc := range tcs {
		metric, label := nameAndLabel(tc.prefix, tc.name)
		assert.Equal(t, tc.wantMetric, metric, tc.prefix+tc.name)
		assert.Equal(t, tc.wantLabel, label, tc.prefix+tc.name)
	}
}

func TestPrometeusize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{
			in:   "serverStatus.wiredTiger.transaction.transaction checkpoint most recent time (msecs)",
			want: "mongodb_ss_wt_txn_transaction_checkpoint_most_recent_time_msecs",
		},
		{
			in:   "serverStatus.wiredTiger.thread-yield.page acquire time sleeping (usecs)",
			want: "mongodb_ss_wt_thread_yield_page_acquire_time_sleeping_usecs",
		},
		{
			in:   "serverStatus.opLatencies.reads.latency",
			want: "mongodb_ss_opLatencies_reads_latency",
		},
		{
			in:   "replSetGetStatus.optimes.lastCommittedOpTime.t",
			want: "mongodb_rs_optimes_lastCommittedOpTime_t",
		},
		{
			in:   "systemMetrics.memory.Active_kb",
			want: "mongodb_sys_memory_Active_kb",
		},
		{
			in:   "local.oplog.rs.stats.wiredTiger.block-manager.checkpoint size",
			want: "mongodb_oplog_stats_wt_block_manager_checkpoint_size",
		},
		{
			in:   "local.oplog.rs.stats.storageSize",
			want: "mongodb_oplog_stats_storageSize",
		},
		{
			in:   "collstats_storage.wiredTiger.xxx",
			want: "mongodb_collstats_storage_wt_xxx",
		},

		{
			in:   "collstats_storage.indexDetails.xxx",
			want: "mongodb_collstats_storage_idx_xxx",
		},
		{
			in:   "collStats.storageStats.xxx",
			want: "mongodb_collstats_storage_xxx",
		},
		{
			in:   "collStats.latencyStats.xxx",
			want: "mongodb_collstats_latency_xxx",
		},
	}

	for _, test := range tests {
		got := prometheusize(test.in)
		assert.Equal(t, test.want, got)
	}
}

// Test supported value types conversion.
func TestMakeRawMetric(t *testing.T) {
	prefix := "serverStatus.transactions."
	name := "retriedCommandsCount"
	testCases := []struct {
		value   interface{}
		wantVal *float64
	}{
		{value: true, wantVal: pointer.ToFloat64(1)},
		{value: false, wantVal: pointer.ToFloat64(0)},
		{value: int32(1), wantVal: pointer.ToFloat64(1)},
		{value: int64(2), wantVal: pointer.ToFloat64(2)},
		{value: float32(1.23), wantVal: pointer.ToFloat64(float64(float32(1.23)))},
		{value: float64(1.23), wantVal: pointer.ToFloat64(1.23)},
		{value: primitive.A{}, wantVal: nil},
		{value: primitive.Timestamp{}, wantVal: nil},
		{value: "zapp", wantVal: nil},
		{value: []byte{}, wantVal: nil},
		{value: time.Date(2020, 6, 15, 0, 0, 0, 0, time.UTC), wantVal: nil},
	}

	ln := make([]string, 0) // needs pre-allocation to accomplish pre-allocation for labels
	lv := make([]string, 0)

	fqName := prometheusize(prefix + name)
	help := metricHelp(prefix, name)

	for _, tc := range testCases {
		var want *rawMetric
		if tc.wantVal != nil {
			want = &rawMetric{
				fqName: fqName,
				help:   help,
				ln:     ln,
				lv:     lv,
				val:    *tc.wantVal,
				vt:     prometheus.CounterValue,
			}
		}

		m, err := makeRawMetric(prefix, name, tc.value, nil)

		assert.NoError(t, err)
		assert.Equal(t, want, m)
	}
}

func TestRawToCompatibleRawMetric(t *testing.T) {
	testCases := []struct {
		in   *rawMetric
		want *rawMetric
	}{
		{
			in: &rawMetric{
				fqName: "mongodb_ss_opLatencies_commands_latency",
				val:    float64(1),
				vt:     prometheus.UntypedValue,
			},
			want: &rawMetric{
				fqName: "mongodb_ss_opLatencies_latency",
				help:   "mongodb_ss_opLatencies_latency",
				ln:     []string{"op_type"},
				lv:     []string{"commands"},
				val:    1,
				vt:     3,
			},
		},
		{
			in: &rawMetric{
				fqName: "mongodb_ss_opLatencies_commands_ops",
				val:    float64(1),
				vt:     prometheus.UntypedValue,
			},
			want: &rawMetric{
				fqName: "mongodb_ss_opLatencies_ops",
				help:   "mongodb_ss_opLatencies_ops",
				ln:     []string{"op_type"},
				lv:     []string{"commands"},
				val:    1,
				vt:     3,
			},
		},
	}

	for _, tc := range testCases {
		m := metricRenameAndLabel(tc.in, specialConversions())
		assert.Equal(t, m[0], tc.want)
	}
}

func TestExtractHistogram(t *testing.T) {
	t.Run("Valid Histogram", func(t *testing.T) {
		m := primitive.A{
			primitive.M{
				"count":      0,
				"lowerBound": 0,
			},
			primitive.M{
				"lowerBound": 128,
				"count":      0,
			},
			primitive.M{
				"lowerBound": 256,
				"count":      0,
			},
		}
		want := map[histogramLabel][]float64{
			histogramLabel{key: "lowerBound", value: "0"}:   []float64{0},
			histogramLabel{key: "lowerBound", value: "128"}: []float64{0},
			histogramLabel{key: "lowerBound", value: "256"}: []float64{0},
		}

		histograms := extractHistograms(m)
		assert.Equal(t, want, histograms)
	})
}
