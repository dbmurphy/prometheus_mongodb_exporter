# Usage Reference

## Flags
|Flag|Description|Example|
|-----|-----|-----|
|-h, \-\-help|Show context-sensitive help||
|--[no-]compatible-mode|Enable old mongodb-exporter compatible metrics||
|--[no-]discovering-mode|Enable autodiscover collections||
|--mongodb.collstats-colls|List of comma separared databases.collections to get $collStats|--mongodb.collstats-colls=db1,db2.col2|
|--mongodb.indexstats-colls|List of comma separared databases.collections to get $indexStats|--mongodb.indexstats-colls=db1.col1,db2.col2|
|--[no-]mongodb.direct-connect|Whether or not a direct connect should be made. Direct connections are not valid if multiple hosts are specified or an SRV URI is used||
|--[no-]mongodb.global-conn-pool|Use global connection pool instead of creating new pool for each http request||
|--mongodb.uri|MongoDB connection URI ($MONGODB_URI)|--mongodb.uri=mongodb://user:pass@127.0.0.1:27017/admin?ssl=true|
|--web.listen-address|Address to listen on for web interface and telemetry|--web.listen-address=":9216"|
|--web.telemetry-path|Metrics expose path|--web.telemetry-path="/metrics"|
|--web.config|Path to the file having Prometheus TLS config for basic auth|--web.config=STRING|
|--web.timeout-offset|Offset to subtract from the timeout in seconds|--web.timeout-offset=1|
|--log.level|Only log messages with the given severity or above. Valid levels: [debug, info, warn, error, fatal]|--log.level="error"|
|--collector.diagnosticdata|Enable collecting metrics from getDiagnosticData|
|--collector.replicasetstatus|Enable collecting metrics from replSetGetStatus|
|--collector.serverstatus|Enable collecting metrics from serverStatus|
|--collector.dbstats|Enable collecting metrics from dbStats||
|--collector.dbstatsfreestorage|Enable collecting freeStorage metrics from dbStats. If the instance has a large number of collections or indexes, obtaining free space usage data may cause processing delays||
|--collector.topmetrics|Enable collecting metrics from top admin command|
|--collector.currentopmetrics|Enable collecting metrics from currentop admin command|
|--collector.indexstats|Enable collecting metrics from $indexStats|
|--collector.collstats|Enable collecting metrics from $collStats|
|--collect-all|Enable all collectors. Same as specifying all --collector.\<name\>|
|--collector.collstats-limit=0|Disable collstats, dbstats, topmetrics and indexstats collector if there are more than \<n\> collections. 0=No limit|
|--collector.profile-time-ts=30|Set time for scrape slow queries| This interval must be synchronized with the Prometheus scrape interval|
|--collector.profile|Enable collecting metrics from profile|
|--collector.shards|Enable collecting metrics related to Mongo shards|
|--metrics.overridedescendingindex| Enable descending index name override to replace -1 with _DESC ||
|--version|Show version and exit|
