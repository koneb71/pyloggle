package clickhouse

import (
	"context"
	"time"

	"pyloggle/internal/api"
)

// TableInfo is one table's footprint from system.parts (active parts only).
type TableInfo struct {
	Name            string `json:"name"`
	Rows            uint64 `json:"rows"`
	Bytes           uint64 `json:"bytes"`
	Uncompressed    uint64 `json:"uncompressed"`
	Parts           uint64 `json:"parts"`
	OldestPartition string `json:"oldest_partition,omitempty"`
}

// SystemInfo is what the collector reads every minute for /metrics and the System page.
type SystemInfo struct {
	Version          string      `json:"version"`
	DiskFree         uint64      `json:"disk_free"`
	DiskTotal        uint64      `json:"disk_total"`
	MutationsPending int         `json:"mutations_pending"`
	Tables           []TableInfo `json:"tables"`
}

// SystemInfo queries the server version, the default disk, pending mutations and per-table parts.
func (c *Client) SystemInfo(ctx context.Context) (SystemInfo, error) {
	var out SystemInfo
	if err := c.conn.QueryRow(ctx, "SELECT version()").Scan(&out.Version); err != nil {
		return out, err
	}
	_ = c.conn.QueryRow(ctx, "SELECT free_space, total_space FROM system.disks WHERE name = 'default'").Scan(&out.DiskFree, &out.DiskTotal)
	n, err := c.PendingMutations(ctx, "")
	if err != nil {
		return out, err
	}
	out.MutationsPending = n
	rows, err := c.conn.Query(ctx, `SELECT table, sum(rows), sum(bytes_on_disk), sum(data_uncompressed_bytes), count(), min(partition)
		FROM system.parts WHERE database = currentDatabase() AND active GROUP BY table ORDER BY table`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Tables = []TableInfo{}
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Name, &t.Rows, &t.Bytes, &t.Uncompressed, &t.Parts, &t.OldestPartition); err != nil {
			return out, err
		}
		out.Tables = append(out.Tables, t)
	}
	return out, rows.Err()
}

// GlobalLogVolume counts log lines per bucket across every project (the System page's ingest
// rate), from the minutely pre-aggregate.
func (c *Client) GlobalLogVolume(ctx context.Context, from, to time.Time, bucketSeconds int) ([]api.TimeSeriesPoint, error) {
	if bucketSeconds < 60 {
		bucketSeconds = 60
	}
	rows, err := c.conn.Query(ctx, `SELECT toStartOfInterval(bucket, INTERVAL ? SECOND) AS b, sum(cnt) FROM log_counts_minutely
		WHERE bucket >= ? AND bucket < ? GROUP BY b ORDER BY b`, bucketSeconds, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	got := map[time.Time]uint64{}
	for rows.Next() {
		var b time.Time
		var n uint64
		if err := rows.Scan(&b, &n); err != nil {
			return nil, err
		}
		got[b.UTC()] = n
	}
	return fillSeries(from, to, time.Duration(bucketSeconds)*time.Second, got), rows.Err()
}
