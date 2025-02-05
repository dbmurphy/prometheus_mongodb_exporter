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

// Package exporter implements the collectors and metrics handlers.
package exporter

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/percona/mongodb_exporter/exporter/dsn_fix"
)

// Exporter holds Exporter methods and attributes.
type Exporter struct {
	client                *mongo.Client
	clientMu              sync.Mutex
	logger                *logrus.Logger
	opts                  *Opts
	lock                  *sync.Mutex
	totalCollectionsCount int
}

// Opts holds new exporter options.
type Opts struct {
	CompatibleMode         bool
	DirectConnect          bool
	ConnectTimeoutMS       int
	DisableDefaultRegistry bool
	DiscoveringMode        bool
	GlobalConnPool         bool
	TimeoutOffset          int

	CollectAll               bool
	EnableDBStats            bool
	EnableDBStatsFreeStorage bool
	EnableDiagnosticData     bool
	EnableReplicasetStatus   bool
	EnableReplicasetConfig   bool
	EnableCurrentopMetrics   bool
	EnableTopMetrics         bool
	EnableIndexStats         bool
	EnableCollStats          bool
	EnableProfile            bool
	EnableShards             bool
	EnableFCV                bool // Feature Compatibility Version.
	EnablePBMMetrics         bool

	EnableOverrideDescendingIndex bool

	// Only get stats for the collections matching this list of namespaces.
	// Example: db1.col1,db.col1
	CollStatsNamespaces    []string
	CollStatsLimit         int
	CollStatsEnableDetails bool
	IndexStatsCollections  []string
	CurrentOpSlowTime      string
	ProfileTimeTS          int

	Logger *logrus.Logger

	URI      string
	NodeName string
}

var (
	errCannotHandleType   = fmt.Errorf("don't know how to handle data type")
	errUnexpectedDataType = fmt.Errorf("unexpected data type")
)

const (
	defaultCacheSize = 1000
)

// New connects to the database and returns a new Exporter instance.
func New(opts *Opts) *Exporter {
	if opts == nil {
		opts = new(Opts)
	}

	if opts.Logger == nil {
		opts.Logger = logrus.New()
	}

	ctx := context.Background()

	exp := &Exporter{
		logger:                opts.Logger,
		opts:                  opts,
		lock:                  &sync.Mutex{},
		totalCollectionsCount: -1, // Not calculated yet. waiting the db connection.
	}
	// Try initial connect. Connection will be retried with every scrape.
	go func() {
		_, err := exp.getClient(ctx)
		if err != nil {
			exp.logger.Errorf("Cannot connect to MongoDB: %v", err)
		}
	}()

	return exp
}

func (e *Exporter) getTotalCollectionsCount() int {
	e.lock.Lock()
	defer e.lock.Unlock()

	return e.totalCollectionsCount
}

func (e *Exporter) makeRegistry(ctx context.Context, client *mongo.Client, topologyInfo labelsGetter, requestOpts Opts) *prometheus.Registry {
	registry := prometheus.NewRegistry()

	nodeType, err := getNodeType(ctx, client)
	if err != nil {
		e.logger.Errorf("Registry - Cannot get node type : %s", err)
	}

	dbBuildInfo, err := retrieveMongoDBBuildInfo(ctx, client, e.logger.WithField("component", "buildInfo"))
	if err != nil {
		e.logger.Warnf("Registry - Cannot get MongoDB buildInfo: %s", err)
	}

	gc := newGeneralCollector(ctx, client, nodeType, e.opts.Logger)
	registry.MustRegister(gc)

	// Enable collectors like collstats and indexstats depending on the number of collections
	// present in the database.
	limitsOk := false
	if e.opts.CollStatsLimit <= 0 || // Unlimited
		e.getTotalCollectionsCount() <= e.opts.CollStatsLimit {
		limitsOk = true
	}

	if e.opts.CollectAll {
		if len(e.opts.CollStatsNamespaces) == 0 {
			e.opts.DiscoveringMode = true
		}
		e.opts.EnableDiagnosticData = true
		e.opts.EnableDBStats = true
		e.opts.EnableDBStatsFreeStorage = true
		e.opts.EnableCollStats = true
		e.opts.EnableTopMetrics = true
		e.opts.EnableReplicasetStatus = true
		e.opts.EnableReplicasetConfig = true
		e.opts.EnableIndexStats = true
		e.opts.EnableCurrentopMetrics = true
		e.opts.EnableProfile = true
		e.opts.EnableShards = true
		e.opts.EnableFCV = true
		e.opts.EnablePBMMetrics = true
	}

	// arbiter only have isMaster privileges
	if nodeType == typeArbiter {
		e.opts.EnableDBStats = false
		e.opts.EnableDBStatsFreeStorage = false
		e.opts.EnableCollStats = false
		e.opts.EnableTopMetrics = false
		e.opts.EnableReplicasetStatus = false
		e.opts.EnableIndexStats = false
		e.opts.EnableCurrentopMetrics = false
		e.opts.EnableProfile = false
		e.opts.EnableShards = false
		e.opts.EnableFCV = false
		e.opts.EnablePBMMetrics = false
	}

	// If we manually set the collection names we want or auto discovery is set.
	if (len(e.opts.CollStatsNamespaces) > 0 || e.opts.DiscoveringMode) && e.opts.EnableCollStats && limitsOk && requestOpts.EnableCollStats {
		cc := newCollectionStatsCollector(ctx, client, e.opts.Logger,
			e.opts.DiscoveringMode,
			topologyInfo, e.opts.CollStatsNamespaces, e.opts.CollStatsEnableDetails)
		registry.MustRegister(cc)
	}

	// If we manually set the collection names we want or auto discovery is set.
	if (len(e.opts.IndexStatsCollections) > 0 || e.opts.DiscoveringMode) && e.opts.EnableIndexStats && limitsOk && requestOpts.EnableIndexStats {
		ic := newIndexStatsCollector(ctx, client, e.opts.Logger,
			e.opts.DiscoveringMode, e.opts.EnableOverrideDescendingIndex,
			topologyInfo, e.opts.IndexStatsCollections)
		registry.MustRegister(ic)
	}

	if e.opts.EnableDiagnosticData && requestOpts.EnableDiagnosticData {
		ddc := newDiagnosticDataCollector(ctx, client, e.opts.Logger,
			e.opts.CompatibleMode, topologyInfo, dbBuildInfo)
		registry.MustRegister(ddc)
	}

	if e.opts.EnableDBStats && limitsOk && requestOpts.EnableDBStats {
		cc := newDBStatsCollector(ctx, client, e.opts.Logger,
			e.opts.CompatibleMode, topologyInfo, nil, e.opts.EnableDBStatsFreeStorage)
		registry.MustRegister(cc)
	}

	if e.opts.EnableCurrentopMetrics && nodeType != typeMongos && limitsOk && requestOpts.EnableCurrentopMetrics && e.opts.CurrentOpSlowTime != "" {
		coc := newCurrentopCollector(ctx, client, e.opts.Logger,
			e.opts.CompatibleMode, topologyInfo, e.opts.CurrentOpSlowTime)
		registry.MustRegister(coc)
	}

	if e.opts.EnableProfile && nodeType != typeMongos && limitsOk && requestOpts.EnableProfile && e.opts.ProfileTimeTS != 0 {
		pc := newProfileCollector(ctx, client, e.opts.Logger,
			e.opts.CompatibleMode, topologyInfo, e.opts.ProfileTimeTS)
		registry.MustRegister(pc)
	}

	if e.opts.EnableTopMetrics && nodeType != typeMongos && limitsOk && requestOpts.EnableTopMetrics {
		tc := newTopCollector(ctx, client, e.opts.Logger,
			e.opts.CompatibleMode, topologyInfo)
		registry.MustRegister(tc)
	}

	// replSetGetStatus is not supported through mongos.
	if e.opts.EnableReplicasetStatus && nodeType != typeMongos && requestOpts.EnableReplicasetStatus {
		rsgsc := newReplicationSetStatusCollector(ctx, client, e.opts.Logger,
			e.opts.CompatibleMode, topologyInfo)
		registry.MustRegister(rsgsc)
	}

	// replSetGetStatus is not supported through mongos.
	if e.opts.EnableReplicasetConfig && nodeType != typeMongos && requestOpts.EnableReplicasetConfig {
		rsgsc := newReplicationSetConfigCollector(ctx, client, e.opts.Logger,
			e.opts.CompatibleMode, topologyInfo)
		registry.MustRegister(rsgsc)
	}
	if e.opts.EnableShards && nodeType == typeMongos && requestOpts.EnableShards {
		sc := newShardsCollector(ctx, client, e.opts.Logger, e.opts.CompatibleMode)
		registry.MustRegister(sc)
	}

	if e.opts.EnableFCV && nodeType != typeMongos {
		fcvc := newFeatureCompatibilityCollector(ctx, client, e.opts.Logger)
		registry.MustRegister(fcvc)
	}

	if e.opts.EnablePBMMetrics && requestOpts.EnablePBMMetrics {
		pbmc := newPbmCollector(ctx, client, e.opts.URI, e.opts.Logger)
		registry.MustRegister(pbmc)
	}

	return registry
}

func (e *Exporter) getClient(ctx context.Context) (*mongo.Client, error) {
	if e.opts.GlobalConnPool {
		// Get global client. Maybe it must be initialized first.
		// Initialization is retried with every scrape until it succeeds once.
		e.clientMu.Lock()
		defer e.clientMu.Unlock()

		// If client is already initialized, return it.
		if e.client != nil {
			return e.client, nil
		}

		client, err := connect(context.Background(), e.opts)
		if err != nil {
			return nil, err
		}
		e.client = client

		return client, nil
	}

	// !e.opts.GlobalConnPool: create new client for every scrape.
	client, err := connect(ctx, e.opts)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// Handler returns an http.Handler that serves metrics. Can be used instead of
// run for hooking up custom HTTP servers.
func (e *Exporter) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seconds, err := strconv.Atoi(r.Header.Get("X-Prometheus-Scrape-Timeout-Seconds"))
		// To support older ones vmagents.
		if err != nil {
			seconds = 10
		}
		seconds -= e.opts.TimeoutOffset

		var client *mongo.Client
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(seconds)*time.Second)
		defer cancel()

		requestOpts := GetRequestOpts(r.URL.Query()["collect[]"], e.opts)

		client, err = e.getClient(ctx)
		if err != nil {
			e.logger.Errorf("Cannot connect to MongoDB: %v", err)
		}

		if client != nil && e.getTotalCollectionsCount() <= 0 {
			count, err := nonSystemCollectionsCount(ctx, client, nil, nil)
			if err == nil {
				e.lock.Lock()
				e.totalCollectionsCount = count
				e.lock.Unlock()
			}
		}

		// Close client after usage.
		if !e.opts.GlobalConnPool {
			defer func() {
				if client != nil {
					err := client.Disconnect(ctx)
					if err != nil {
						e.logger.Errorf("Cannot disconnect client: %v", err)
					}
				}
			}()
		}

		var gatherers prometheus.Gatherers

		if !e.opts.DisableDefaultRegistry {
			gatherers = append(gatherers, prometheus.DefaultGatherer)
		}

		var registry *prometheus.Registry
		var ti *topologyInfo
		if client != nil {
			// Topology can change between requests, so we need to get it every time.
			ti = newTopologyInfo(ctx, client, e.logger)
			registry = e.makeRegistry(ctx, client, ti, requestOpts)
		} else {
			registry = prometheus.NewRegistry()
			gc := newGeneralCollector(ctx, client, "", e.opts.Logger)
			registry.MustRegister(gc)
		}

		gatherers = append(gatherers, registry)

		// Delegate http serving to Prometheus client library, which will call collector.Collect.
		h := promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{
			ErrorHandling: promhttp.ContinueOnError,
			ErrorLog:      e.logger,
		})

		h.ServeHTTP(w, r)
	})
}

// GetRequestOpts makes exporter.Opts structure from request filters and default options.
func GetRequestOpts(filters []string, defaultOpts *Opts) Opts {
	requestOpts := Opts{}

	if len(filters) == 0 {
		requestOpts = *defaultOpts
	}

	for _, filter := range filters {
		switch filter {
		case "diagnosticdata":
			requestOpts.EnableDiagnosticData = true
		case "replicasetstatus":
			requestOpts.EnableReplicasetStatus = true
		case "replicasetconfig":
			requestOpts.EnableReplicasetConfig = true
		case "dbstats":
			requestOpts.EnableDBStats = true
		case "topmetrics":
			requestOpts.EnableTopMetrics = true
		case "currentopmetrics":
			requestOpts.EnableCurrentopMetrics = true
		case "indexstats":
			requestOpts.EnableIndexStats = true
		case "collstats":
			requestOpts.EnableCollStats = true
		case "profile":
			requestOpts.EnableProfile = true
		case "shards":
			requestOpts.EnableShards = true
		case "fcv":
			requestOpts.EnableFCV = true
		case "pbm":
			requestOpts.EnablePBMMetrics = true
		}
	}

	return requestOpts
}

func connect(ctx context.Context, opts *Opts) (*mongo.Client, error) {
	clientOpts, err := dsn_fix.ClientOptionsForDSN(opts.URI)
	if err != nil {
		return nil, fmt.Errorf("invalid dsn: %w", err)
	}

	clientOpts.SetDirect(opts.DirectConnect)
	clientOpts.SetAppName("mongodb_exporter")

	if clientOpts.ConnectTimeout == nil {
		connectTimeout := time.Duration(opts.ConnectTimeoutMS) * time.Millisecond
		clientOpts.SetConnectTimeout(connectTimeout)
		clientOpts.SetServerSelectionTimeout(connectTimeout)
	}

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("invalid MongoDB options: %w", err)
	}

	if err = client.Ping(ctx, nil); err != nil {
		// Ping failed. Close background connections. Error is ignored since the ping error is more relevant.
		_ = client.Disconnect(ctx)

		return nil, fmt.Errorf("cannot connect to MongoDB: %w", err)
	}

	return client, nil
}
