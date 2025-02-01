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
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/percona/mongodb_exporter/internal/proto"
	"github.com/percona/mongodb_exporter/internal/util"
)

type mongoDBNodeType string

const (
	labelClusterRole     = "cl_role"
	labelClusterID       = "cl_id"
	labelReplicasetName  = "rs_nm"
	labelReplicasetState = "rs_state"

	typeIsDBGrid                    = "isdbgrid"
	typeMongos      mongoDBNodeType = "mongos"
	typeMongod      mongoDBNodeType = "mongod"
	typeShardServer mongoDBNodeType = "shardsvr"
	typeArbiter     mongoDBNodeType = "arbiter"
	typeOther       mongoDBNodeType = ""
)

type labelsGetter interface {
	baseLabels() map[string]string
	loadLabels(context.Context) error
}

// This is an object to make it posible to easily reload the labels in case of
// disconnection from the db. Just call loadLabels when required.
type topologyInfo struct {
	// TODO: with https://jira.percona.com/browse/PMM-6435, replace this client pointer
	// by a new connector, able to reconnect if needed. In case of reconnection, we should
	// call loadLabels to refresh the labels because they might have changed
	client *mongo.Client
	logger *logrus.Entry
	rw     sync.RWMutex
	labels map[string]string
}

// ErrCannotGetTopologyLabels Cannot read topology labels.
var ErrCannotGetTopologyLabels = fmt.Errorf("cannot get topology labels")

func newTopologyInfo(ctx context.Context, client *mongo.Client, logger *logrus.Logger) *topologyInfo {
	ti := &topologyInfo{
		client: client,
		logger: logger.WithFields(logrus.Fields{"component": "topology_info"}),
		labels: make(map[string]string),
		rw:     sync.RWMutex{},
	}

	err := ti.loadLabels(ctx)
	if err != nil {
		logger.Warnf("cannot load topology labels: %s", err)
	}

	return ti
}

// baseLabels returns a copy of the topology labels because in some collectors like
// collstats collector, we must use these base labels and add the namespace or other labels.
func (t *topologyInfo) baseLabels() map[string]string {
	c := map[string]string{}

	t.rw.RLock()
	for k, v := range t.labels {
		c[k] = v
	}
	t.rw.RUnlock()

	return c
}

// TopologyLabels reads several values from MongoDB instance like replicaset name, and other
// topology information and returns a map of labels used to better identify the current monitored instance.
func (t *topologyInfo) loadLabels(ctx context.Context) error {
	t.rw.Lock()
	defer t.rw.Unlock()

	t.labels = make(map[string]string)

	role, err := getClusterRole(ctx, t.client)
	if err != nil {
		return errors.Wrap(err, "cannot get node type for topology info")
	}

	t.labels[labelClusterRole] = role

	// Standalone instances or mongos instances won't have a replicaset name
	if rs, err := util.ReplicasetConfig(ctx, t.client); err == nil {
		t.labels[labelReplicasetName] = rs.Config.ID
	}

	nodeType, err := getNodeType(ctx, t.client)
	if err != nil {
		return err
	}

	cid, err := util.ClusterID(ctx, t.client)
	if err != nil {
		if nodeType != typeArbiter { // arbiters don't have a cluster ID
			return errors.Wrapf(ErrCannotGetTopologyLabels, "error getting cluster ID: %s", err)
		}
	}
	t.labels[labelClusterID] = cid

	// Standalone instances or mongos instances won't have a replicaset state
	_, state, err := util.MyState(ctx, t.client)
	if err == nil {
		t.labels[labelReplicasetState] = fmt.Sprintf("%d", state)
	}

	return nil
}

func getNodeType(ctx context.Context, client *mongo.Client) (mongoDBNodeType, error) {
	if client == nil {
		return "", errors.New("cannot get mongo node type from an empty client")
	}
	md := proto.MasterDoc{}
	if err := client.Database("admin").RunCommand(ctx, primitive.M{"isMaster": 1}).Decode(&md); err != nil {
		if e, ok := err.(mongo.CommandError); ok {
			if e.Code == Unauthorized {
				errors.New("unauthorized to getisMaster")
				os.Exit(1)
			}
		}
		return "", err
	}

	if md.ArbiterOnly {
		return typeArbiter, nil
	} else if md.Msg == typeIsDBGrid {
		// isdbgrid is always the msg value when calling isMaster on a mongos
		// see http://docs.mongodb.org/manual/core/sharded-cluster-query-router/
		return typeMongos, nil
	}

	return typeMongod, nil
}

func getClusterRole(ctx context.Context, client *mongo.Client) (string, error) {
	cmdOpts := primitive.M{}
	// Not always we can get this info. For example, we cannot get this for hidden hosts so
	// if there is an error, just ignore it
	res := client.Database("admin").RunCommand(ctx, primitive.D{
		{Key: "getCmdLineOpts", Value: 1},
		{Key: "recordStats", Value: 1},
	})

	if res.Err() != nil {
		return "", nil
	}

	if err := res.Decode(&cmdOpts); err != nil {
		if e, ok := err.(mongo.CommandError); ok {
			if e.Code == Unauthorized {
				errors.New("unauthorized to getCmdLineOpts")
				os.Exit(1)
			}
		}
		return "", errors.Wrap(err, "cannot decode getCmdLineOpts response")
	}

	if walkTo(cmdOpts, []string{"parsed", "sharding", "configDB"}) != nil {
		return "mongos", nil
	}

	clusterRole := ""
	if cr := walkTo(cmdOpts, []string{"parsed", "sharding", "clusterRole"}); cr != nil {
		clusterRole, _ = cr.(string)
	}

	return clusterRole, nil
}
