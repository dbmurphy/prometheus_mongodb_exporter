// mongodb_exporter
// Copyright (C) 2022 Percona LLC
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
	"sort"
	"strings"

	"github.com/AlekSi/pointer"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var systemDBs = []string{"admin", "config", "local"} //nolint:gochecknoglobals

func listCollections(ctx context.Context, client *mongo.Client, database string, filterInNamespaces []string) ([]string, error) {
	filter := bson.D{} // Default=empty -> list all collections

	// if there is a filter with the list of collections we want, create a filter like
	// $or: {
	//     {"$regex": "collection1"},
	//     {"$regex": "collection2"},
	// }
	if len(filterInNamespaces) > 0 {
		matchExpressions := []bson.D{}

		for _, namespace := range filterInNamespaces {
			parts := strings.Split(namespace, ".") // db.collection.name.with.dots
			if len(parts) > 1 {
				// The part before the first dot is the database name.
				// The rest is the collection name and it can have dots. We need to rebuild it.
				collection := strings.Join(parts[1:], ".")
				matchExpressions = append(matchExpressions,
					bson.D{{Key: "name", Value: primitive.Regex{Pattern: collection, Options: "i"}}})
			}
		}

		if len(matchExpressions) > 0 {
			filter = bson.D{{Key: "$or", Value: matchExpressions}}
		}
	}

	collections, err := client.Database(database).ListCollectionNames(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get the list of collections for discovery")
	}

	return collections, nil
}

// databases returns the list of databases matching the filters.
// - filterInNamespaces: Include only the database names matching the any of the regular expressions in this list.
//
//	Case will be ignored because the function will automatically add the ignore case
//	flag to the regular expression.
//
// - exclude: List of databases to be excluded. Useful to ignore system databases.
func databases(ctx context.Context, client *mongo.Client, filterInNamespaces []string, exclude []string) ([]string, error) {
	opts := &options.ListDatabasesOptions{NameOnly: pointer.ToBool(true), AuthorizedDatabases: pointer.ToBool(true)}

	filter := bson.D{}

	if excludeFilter := makeExcludeFilter(exclude); excludeFilter != nil {
		filter = append(filter, *excludeFilter)
	}

	if namespacesFilter := makeDBsFilter(filterInNamespaces); namespacesFilter != nil {
		filter = append(filter, *namespacesFilter)
	}

	dbNames, err := client.ListDatabaseNames(ctx, filter, opts)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get the database names list")
	}

	return dbNames, nil
}

func makeExcludeFilter(exclude []string) *primitive.E {
	filterExpressions := []bson.D{}
	for _, dbname := range exclude {
		filterExpressions = append(filterExpressions,
			bson.D{{Key: "name", Value: bson.D{{Key: "$ne", Value: dbname}}}},
		)
	}

	if len(filterExpressions) == 0 {
		return nil
	}

	return &primitive.E{Key: "$and", Value: filterExpressions}
}

func makeDBsFilter(filterInNamespaces []string) *primitive.E {
	filterExpressions := []bson.D{}

	nss := removeEmptyStrings(filterInNamespaces)
	for _, namespace := range nss {
		parts := strings.Split(namespace, ".")
		filterExpressions = append(filterExpressions,
			bson.D{{Key: "name", Value: bson.D{{Key: "$eq", Value: parts[0]}}}},
		)
	}

	if len(filterExpressions) == 0 {
		return nil
	}

	return &primitive.E{Key: "$or", Value: filterExpressions}
}

func removeEmptyStrings(items []string) []string {
	cleanList := []string{}

	for _, item := range items {
		if item == "" {
			continue
		}
		cleanList = append(cleanList, item)
	}

	return cleanList
}

func unique(slice []string) []string {
	keys := make(map[string]bool)
	list := []string{}

	for _, entry := range slice {
		if _, ok := keys[entry]; !ok {
			keys[entry] = true
			list = append(list, entry)
		}
	}

	return list
}

func listCollectionsWithoutViews(ctx context.Context, client *mongo.Client) (map[string]struct{}, error) {
	dbs, err := databases(ctx, client, nil, nil)
	if err != nil {
		return nil, errors.Wrap(err, "cannot make the list of databases to list all collections")
	}

	res := make(map[string]struct{})
	for _, db := range dbs {
		if db == "" {
			continue
		}

		collections, err := client.Database(db).ListCollectionNames(ctx, bson.M{"type": "collection"})
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("cannot get the list of collections from database %s", db))
		}
		for _, collection := range collections {
			res[fmt.Sprintf("%s.%s", db, collection)] = struct{}{}
		}
	}

	return res, nil
}

func filterCollectionsWithoutViews(ctx context.Context, client *mongo.Client, collections []string) ([]string, error) {
	onlyCollections, err := listCollectionsWithoutViews(ctx, client)
	if err != nil {
		return nil, err
	}

	filteredCollections := []string{}
	for _, collection := range collections {
		if _, ok := onlyCollections[collection]; !ok {
			return nil, fmt.Errorf("collection/namespace %s is view. Cannot be used for collstats/indexstats", collection)
		}

		filteredCollections = append(filteredCollections, collection)
	}

	return filteredCollections, nil
}

func listAllCollections(ctx context.Context, client *mongo.Client, filterInNamespaces []string, excludeDBs []string) (map[string][]string, error) {
	namespaces := make(map[string][]string)

	dbs, err := databases(ctx, client, filterInNamespaces, excludeDBs)
	if err != nil {
		return nil, errors.Wrap(err, "cannot make the list of databases to list all collections")
	}

	filterNS := removeEmptyStrings(filterInNamespaces)

	// If there are no specified namespaces to search for collections, it means all dbs should be included.
	if len(filterNS) == 0 {
		filterNS = append(filterNS, dbs...)
	}

	for _, db := range dbs {
		for _, namespace := range filterNS {
			parts := strings.Split(namespace, ".")
			dbname := strings.TrimSpace(parts[0])

			if dbname == "" || dbname != db {
				continue
			}

			colls, err := listCollections(ctx, client, db, []string{namespace})
			if err != nil {
				return nil, errors.Wrapf(err, "cannot list the collections for %q", db)
			}

			if _, ok := namespaces[db]; !ok {
				namespaces[db] = []string{}
			}

			namespaces[db] = append(namespaces[db], colls...)
		}
	}

	// Make it testable.
	for db, colls := range namespaces {
		uc := unique(colls)
		sort.Strings(uc)
		namespaces[db] = uc
	}

	return namespaces, nil
}

func nonSystemCollectionsCount(ctx context.Context, client *mongo.Client, includeNamespaces []string, filterInCollections []string) (int, error) {
	databases, err := databases(ctx, client, includeNamespaces, systemDBs)
	if err != nil {
		return 0, errors.Wrap(err, "cannot retrieve the collection names for count collections")
	}

	var count int

	for _, dbname := range databases {
		colls, err := listCollections(ctx, client, dbname, filterInCollections)
		if err != nil {
			return 0, errors.Wrap(err, "cannot get collections count")
		}
		count += len(colls)
	}

	return count, nil
}

func splitNamespace(ns string) (database, collection string) {
	parts := strings.Split(ns, ".")
	if len(parts) < 2 { // there is no collection?
		return parts[0], ""
	}

	return parts[0], strings.Join(parts[1:], ".")
}
