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
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type currentopCollector struct {
	ctx               context.Context
	base              *baseCollector
	compatibleMode    bool
	topologyInfo      labelsGetter
	currentopslowtime string
}

var ErrInvalidOrMissingInprogEntry = errors.New("invalid or missing inprog entry in currentop results")

// newCurrentopCollector creates a collector for being processed queries.
func newCurrentopCollector(ctx context.Context, client *mongo.Client, logger *logrus.Logger,
	compatible bool, topology labelsGetter, currentOpSlowTime string,
) *currentopCollector {
	return &currentopCollector{
		ctx:               ctx,
		base:              newBaseCollector(client, logger.WithFields(logrus.Fields{"collector": "currentop"})),
		compatibleMode:    compatible,
		topologyInfo:      topology,
		currentopslowtime: currentOpSlowTime,
	}
}

func (d *currentopCollector) Describe(ch chan<- *prometheus.Desc) {
	d.base.Describe(d.ctx, ch, d.collect)
}

func (d *currentopCollector) Collect(ch chan<- prometheus.Metric) {
	d.base.Collect(ch)
}

func (d *currentopCollector) collect(ch chan<- prometheus.Metric) {
	defer measureCollectTime(ch, "mongodb", "currentop")()

	logger := d.base.logger
	client := d.base.client
	slowtime, err := time.ParseDuration(d.currentopslowtime)
	if err != nil {
		logger.Errorf("Failed to parse slowtime: %s", err)
		ch <- prometheus.NewInvalidMetric(prometheus.NewInvalidDesc(err), err)
		return
	}
	slowtimems := slowtime.Microseconds()

	// Get all requests that are being processed except system requests (admin and local).
	cmd := bson.D{
		{Key: "currentOp", Value: true},
		{Key: "active", Value: true},
		{Key: "microsecs_running", Value: bson.D{
			{Key: "$exists", Value: true},
			{Key: "$gte", Value: slowtimems},
		}},
		{Key: "op", Value: bson.D{{Key: "$ne", Value: ""}}},
		{Key: "ns", Value: bson.D{
			{Key: "$ne", Value: ""},
			{Key: "$not", Value: bson.D{{Key: "$regex", Value: "^admin.*|^local.*"}}},
		}},
	}
	res := client.Database("admin").RunCommand(d.ctx, cmd)

	var r primitive.M
	if err := res.Decode(&r); err != nil {
		if e, ok := err.(mongo.CommandError); ok {
			if e.Code == Unauthorized {
				logger.Errorf("unauthorized to run currtop: %s", err)
				os.Exit(1)
			}
		}

		logger.Errorf("Failed to decode currentOp response: %s", err)
		ch <- prometheus.NewInvalidMetric(prometheus.NewInvalidDesc(err), err)
		return
	}

	logger.Debug("currentop response from MongoDB:")
	debugResult(logger, r)

	inprog, ok := r["inprog"].(primitive.A)

	if !ok {
		logger.Errorf("Invalid type primitive.A assertion for 'inprog': %T", r["inprog"])
		ch <- prometheus.NewInvalidMetric(prometheus.NewInvalidDesc(ErrInvalidOrMissingInprogEntry),
			ErrInvalidOrMissingInprogEntry)
	}

	labels := d.topologyInfo.baseLabels()
	ln := []string{"opid", "op", "desc", "database", "collection", "ns"}
	const name = "mongodb_currentop_query_uptime"
	pd := prometheus.NewDesc(name, " mongodb_currentop_query_uptime currentop_query", ln, labels)

	for _, bsonMap := range inprog {

		bsonMapElement, ok := bsonMap.(primitive.M)
		if !ok {
			logger.Errorf("Invalid type primitive.M assertion for bsonMap: %T", bsonMapElement)
			continue
		}
		opid, ok := bsonMapElement["opid"].(int32)
		if !ok {
			logger.Errorf("Invalid type int32 assertion for 'opid': %T", bsonMapElement)
			continue
		}
		namespace, ok := bsonMapElement["ns"].(string)
		if !ok {
			logger.Errorf("Invalid type string assertion for 'ns': %T", bsonMapElement)
			continue
		}
		db, collection := splitNamespace(namespace)
		op, ok := bsonMapElement["op"].(string)
		if !ok {
			logger.Errorf("Invalid type string assertion for 'op': %T", bsonMapElement)
			continue
		}
		desc, ok := bsonMapElement["desc"].(string)
		if !ok {
			logger.Errorf("Invalid type string assertion for 'desc': %T", bsonMapElement)
			continue
		}
		microsecs_running, ok := bsonMapElement["microsecs_running"].(int64)
		if !ok {
			logger.Errorf("Invalid type int64 assertion for 'microsecs_running': %T", bsonMapElement)
			continue
		}

		lv := []string{strconv.Itoa(int(opid)), op, desc, db, collection, namespace}

		ch <- prometheus.MustNewConstMetric(pd, prometheus.GaugeValue, float64(microsecs_running), lv...)
	}
}
