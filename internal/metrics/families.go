package metrics

// The families ingestd exposes. Labels are bounded: project ids, outcome names, doors and queue
// kinds; never service, route or user.
var (
	IngestEvents   = Default.NewCounter("pyloggle_ingest_events_total", "Events processed by the ingest pipeline, by outcome.", "project", "outcome")
	IngestRejected = Default.NewCounter("pyloggle_ingest_rejected_total", "Lines or payloads that could not be decoded.", "door")
	IngestBytes    = Default.NewCounter("pyloggle_ingest_bytes_total", "Raw request bytes accepted by door.", "project", "door")
	IngestRequests = Default.NewCounter("pyloggle_ingest_requests_total", "Ingest HTTP requests by door and status.", "door", "status")
	SpansAccepted  = Default.NewCounter("pyloggle_spans_accepted_total", "OTLP spans accepted for storage.", "project")
	PointsAccepted = Default.NewCounter("pyloggle_metric_points_accepted_total", "OTLP metric points accepted for storage.", "project")

	CHQueueDepth    = Default.NewGauge("pyloggle_ch_queue_depth", "Rows waiting in the ClickHouse write queue.", "kind")
	CHQueueCapacity = Default.NewGauge("pyloggle_ch_queue_capacity", "Capacity of the ClickHouse write queue.", "kind")
	CHRowsWritten   = Default.NewCounter("pyloggle_ch_rows_written_total", "Rows written to ClickHouse.", "kind")
	CHRowsDropped   = Default.NewCounter("pyloggle_ch_rows_dropped_total", "Rows dropped after failed flushes or because the queue was full.", "kind")
	CHFlushFailures = Default.NewCounter("pyloggle_ch_flush_failures_total", "Batch flushes that failed (before retries succeed or give up).", "kind")
	CHFlushSeconds  = Default.NewHistogram("pyloggle_ch_flush_seconds", "Duration of a batch insert.", DefaultBuckets, "kind")
	StreamsActive   = Default.NewGauge("pyloggle_streams_active", "Per-(project, service) ingest queues currently allocated.")
	StreamOverflow  = Default.NewCounter("pyloggle_stream_overflow_total", "Events routed to the shared overflow queue because the stream limit was reached.")

	AlertEvaluations   = Default.NewCounter("pyloggle_alert_evaluations_total", "Alert rule evaluations.", "condition")
	AlertFirings       = Default.NewCounter("pyloggle_alert_firings_total", "Alerts fired.", "condition")
	AlertNotifyFailure = Default.NewCounter("pyloggle_alert_notify_failures_total", "Notifier deliveries that failed.", "notifier")
	AlertEnrichFailure = Default.NewCounter("pyloggle_alert_enrich_failures_total", "Firings sent with less context than usual (a lookup failed, ran out of time or panicked).", "reason")
	AlertLastRun       = Default.NewGauge("pyloggle_alert_last_run_timestamp_seconds", "Unix time of the last alert evaluation pass.")

	CHUp                 = Default.NewGauge("pyloggle_ch_up", "1 when the last ClickHouse probe succeeded.")
	CHTableBytes         = Default.NewGauge("pyloggle_ch_table_bytes", "Bytes on disk per table (active parts).", "table")
	CHTableUncompressed  = Default.NewGauge("pyloggle_ch_table_uncompressed_bytes", "Uncompressed bytes per table (active parts).", "table")
	CHTableRows          = Default.NewGauge("pyloggle_ch_table_rows", "Rows per table (active parts).", "table")
	CHTableParts         = Default.NewGauge("pyloggle_ch_table_parts", "Active parts per table.", "table")
	CHOldestPartitionAge = Default.NewGauge("pyloggle_ch_oldest_partition_age_days", "Age of the oldest partition per table.", "table")
	CHDiskFree           = Default.NewGauge("pyloggle_ch_disk_free_bytes", "Free bytes on ClickHouse's default disk.")
	CHDiskTotal          = Default.NewGauge("pyloggle_ch_disk_total_bytes", "Total bytes on ClickHouse's default disk.")
	CHMutationsPending   = Default.NewGauge("pyloggle_ch_mutations_pending", "Mutations not yet finished.")
	BuildInfo            = Default.NewGauge("pyloggle_build_info", "Build information; always 1.", "version", "go_version", "clickhouse_version")
)
