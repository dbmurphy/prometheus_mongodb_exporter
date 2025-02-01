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
	"os"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type featureCompatibilityCollector struct {
	ctx  context.Context
	base *baseCollector
}

// newProfileCollector creates a collector for being processed queries.
func newFeatureCompatibilityCollector(ctx context.Context, client *mongo.Client, logger *logrus.Logger) *featureCompatibilityCollector {
	return &featureCompatibilityCollector{
		ctx:  ctx,
		base: newBaseCollector(client, logger.WithFields(logrus.Fields{"collector": "featureCompatibility"})),
	}
}

func (d *featureCompatibilityCollector) Describe(ch chan<- *prometheus.Desc) {
	d.base.Describe(d.ctx, ch, d.collect)
}

func (d *featureCompatibilityCollector) Collect(ch chan<- prometheus.Metric) {
	d.base.Collect(ch)
}

func (d *featureCompatibilityCollector) collect(ch chan<- prometheus.Metric) {
	defer measureCollectTime(ch, "mongodb", "fcv")()

	cmd := bson.D{{Key: "getParameter", Value: 1}, {Key: "featureCompatibilityVersion", Value: 1}}
	client := d.base.client
	if client == nil {
		return
	}
	res := client.Database("admin").RunCommand(d.ctx, cmd)

	m := make(map[string]interface{})
	if err := res.Decode(&m); err != nil {
		d.base.logger.Errorf("Failed to decode featureCompatibilityVersion: %v", err)
		ch <- prometheus.NewInvalidMetric(prometheus.NewInvalidDesc(err), err)
		if e, ok := err.(mongo.CommandError); ok {
			if e.Code == Unauthorized {
				d.base.logger.Errorf("Failed to decode featureCompatibilityVersion: %v", err)
				os.Exit(1)
			}
		}
		return
	}

	rawValue := walkTo(m, []string{"featureCompatibilityVersion", "version"})
	if rawValue != nil {
		versionString := fmt.Sprintf("%v", rawValue)
		version, err := strconv.ParseFloat(versionString, 64)
		if err != nil {
			d.base.logger.Errorf("Failed to parse featureCompatibilityVersion: %v", err)
			ch <- prometheus.NewInvalidMetric(prometheus.NewInvalidDesc(err), err)
			return
		}

		d := prometheus.NewDesc("mongodb_fcv_feature_compatibility_version", "Feature compatibility version", []string{"version"}, map[string]string{})
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, version, versionString)
	}
}
