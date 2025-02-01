// mongodb_exporter
// Copyright (C) 2023 Percona LLC
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

package util

import (
	"context"
	"fmt"
	"os"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/x/mongo/driver/topology"

	"github.com/percona/mongodb_exporter/internal/proto"
)

const (
	ErrNotYetInitialized     = int32(94)
	ErrNoReplicationEnabled  = int32(76)
	ErrNotPrimaryOrSecondary = int32(13436)
	ErrNotUnauthorized       = int32(13)
)

// MyState returns the replica set and the instance's state if available.
func MyState(ctx context.Context, client *mongo.Client) (string, int, error) {
	var status proto.ReplicaSetStatus

	err := client.Database("admin").RunCommand(ctx, bson.M{"replSetGetStatus": 1}).Decode(&status)
	if err != nil {
		if e, ok := err.(mongo.CommandError); ok {
			if e.Code == ErrNotUnauthorized {
				fmt.Fprintf(os.Stderr, "unauthorized to run command replSetGetStatus: %v\n", err)
				os.Exit(1)
			}
		}
		return "", 0, err
	}

	return status.Set, int(status.MyState), nil
}

// MyRole returns the role of the mongo instance.
func MyRole(ctx context.Context, client *mongo.Client) (*proto.HelloResponse, error) {
	var role proto.HelloResponse
	err := client.Database("admin").RunCommand(ctx, bson.M{"isMaster": 1}).Decode(&role)
	if err != nil {
		if e, ok := err.(mongo.CommandError); ok {
			if e.Code == ErrNotUnauthorized {
				fmt.Fprintf(os.Stderr, "unauthorized to run command isMaster: %v\n", err)
				os.Exit(1)
			}
		}
		return nil, err
	}

	return &role, nil
}

func ReplicasetConfig(ctx context.Context, client *mongo.Client) (*proto.ReplicasetConfig, error) {
	var rs proto.ReplicasetConfig
	if err := client.Database("admin").RunCommand(ctx, bson.M{"replSetGetConfig": 1}).Decode(&rs); err != nil {
		if e, ok := err.(mongo.CommandError); ok {
			if e.Code == ErrNotUnauthorized {
				fmt.Fprintf(os.Stderr, "unauthorized to run command replSetGetConfig: %v\n", err)
				os.Exit(1)
			}
		}
		return nil, err
	}

	return &rs, nil
}

func IsReplicationNotEnabledError(err mongo.CommandError) bool {
	return err.Code == ErrNotYetInitialized || err.Code == ErrNoReplicationEnabled ||
		err.Code == ErrNotPrimaryOrSecondary
}

func ClusterID(ctx context.Context, client *mongo.Client) (string, error) {
	var cv proto.ConfigVersion
	if err := client.Database("config").Collection("version").FindOne(ctx, bson.M{}).Decode(&cv); err == nil {
		return cv.ClusterID.Hex(), nil
	}

	var si proto.ShardIdentity

	filter := bson.M{"_id": "shardIdentity"}

	if err := client.Database("admin").Collection("system.version").FindOne(ctx, filter).Decode(&si); err == nil {
		return si.ClusterID.Hex(), nil
	}

	rc, err := ReplicasetConfig(ctx, client)
	if err != nil {
		if e, ok := err.(mongo.CommandError); ok && IsReplicationNotEnabledError(e) {
			return "", nil
		}
		if _, ok := err.(topology.ServerSelectionError); ok {
			return "", nil
		}
		return "", err
	}

	return rc.Config.Settings.ReplicaSetID.Hex(), nil
}
