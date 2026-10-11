//go:build integration

package clickhouse_test

// Spans: insert through the pipeline, read back a trace with and without the time hint, list
// traces with a span-level filter and a cursor, stats, errors by trace id, and the TTL.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"pyloggle/internal/config"
	"pyloggle/internal/ingest"
	"pyloggle/internal/logquery"
	"pyloggle/internal/model"
	"pyloggle/internal/storage/clickhouse"
)

func TestIntegrationSpansAndTraces(t *testing.T) {
	c, cleanup := e2eClient(t)
	defer cleanup()
	ctx := context.Background()
	if err := c.ApplyRetention(ctx, clickhouse.MigrateOptions{RetentionSpansDays: 3, Logf: func(string, ...any) {}}); err != nil {
		t.Fatalf("retention: %v", err)
	}
	var engine string
	if err := c.QueryRow(ctx, "SELECT engine_full FROM system.tables WHERE database = currentDatabase() AND name = 'spans'").Scan(&engine); err != nil || !strings.Contains(engine, "toIntervalDay(3)") {
		t.Fatalf("spans TTL: %q %v", engine, err)
	}

	cfg := config.FromEnv()
	cfg.RateLimitRPS = 0
	p := ingest.NewPipeline(cfg, c, c, nil)
	tn := ingest.Tenant{ProjectID: "p1"}
	now := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Minute)

	// 20 traces: a server root, a db child, and for the odd ones a failing client child.
	var spans []model.Span
	for i := 0; i < 20; i++ {
		tid := fmt.Sprintf("%032x", i+1)
		start := now.Add(time.Duration(i) * time.Second)
		status := "ok"
		if i%2 == 1 {
			status = "error"
		}
		spans = append(spans,
			model.Span{Service: "api", Env: "prod", TraceID: tid, SpanID: "r" + fmt.Sprint(i), Name: "GET /orders", Kind: "server", Status: status, StartTS: start, DurationUS: uint64(10000 * (i + 1)),
				HTTPMethod: "GET", HTTPRoute: "/orders", HTTPStatus: 200, Attrs: model.Attrs{"request_id": fmt.Sprint(i)}},
			model.Span{Service: "db", Env: "prod", TraceID: tid, SpanID: "d" + fmt.Sprint(i), ParentSpanID: "r" + fmt.Sprint(i), Name: "SELECT orders", Kind: "client", Status: "unset", StartTS: start.Add(time.Millisecond), DurationUS: 2000,
				Attrs: model.Attrs{"db.system": "postgresql"}, Events: []model.SpanEvent{{TS: start.Add(2 * time.Millisecond), Name: "query", Attrs: model.Attrs{"rows": "3"}}}},
		)
		if i%2 == 1 {
			spans = append(spans, model.Span{Service: "api", Env: "prod", TraceID: tid, SpanID: "c" + fmt.Sprint(i), ParentSpanID: "r" + fmt.Sprint(i), Name: "GET inventory", Kind: "client", Status: "error", StatusMsg: "503", StartTS: start.Add(3 * time.Millisecond), DurationUS: 5000})
		}
	}
	if n, o := p.ProcessSpans(ctx, tn, spans, 1000); n != len(spans) || o != ingest.Accepted {
		t.Fatalf("ProcessSpans: %d %v", n, o)
	}
	// An error occurrence and a log line carrying the first trace's id.
	tid0 := fmt.Sprintf("%032x", 1)
	for _, ev := range []model.Event{
		{Timestamp: model.At(now.Add(5 * time.Millisecond)), Service: "api", Env: "prod", Level: "error", Logger: "api.orders", Message: "boom", TraceID: tid0, Exception: &model.Exception{Type: "ValueError", Message: "boom", Frames: []model.Frame{{Filename: "a.py", Function: "f", Lineno: 1}}}},
		{Timestamp: model.At(now.Add(6 * time.Millisecond)), Service: "api", Env: "prod", Level: "info", Logger: "api.orders", Message: "handled", TraceID: tid0},
	} {
		if o := p.Process(ctx, tn, ev, 100); o != ingest.Accepted {
			t.Fatalf("event: %v", o)
		}
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	// Trace detail with and without the time hint.
	for _, at := range []time.Time{{}, now} {
		got, err := c.TraceSpans(ctx, "p1", tid0, at)
		if err != nil || len(got) != 2 || got[0].SpanID != "r0" || got[1].ParentSpanID != "r0" || got[1].Attrs["db.system"] != "postgresql" || len(got[1].Events) != 1 || got[1].Events[0].Attrs["rows"] != "3" {
			t.Fatalf("TraceSpans(at=%v): %+v %v", at, got, err)
		}
		if got[0].StartTS.UnixMicro() != now.UnixMicro() || got[0].Kind != "server" || got[0].HTTPRoute != "/orders" {
			t.Fatalf("root span: %+v", got[0])
		}
	}
	if got, _ := c.TraceSpans(ctx, "p1", tid0, now.Add(-48*time.Hour)); len(got) != 0 {
		t.Fatalf("a wrong time hint must not find the trace: %d", len(got))
	}
	errs, err := c.TraceErrors(ctx, "p1", tid0, now)
	if err != nil || len(errs) != 1 || errs[0].ExceptionType != "ValueError" {
		t.Fatalf("TraceErrors: %+v %v", errs, err)
	}

	// List: newest first, trace-wide counts, keyset cursor.
	from, to := now.Add(-time.Hour), now.Add(time.Hour)
	page, err := c.TraceList(ctx, clickhouse.SpanQuery{ProjectID: "p1", From: from, To: to, Limit: 15})
	if err != nil || len(page) != 15 {
		t.Fatalf("TraceList: %d %v", len(page), err)
	}
	if page[0].TraceID != fmt.Sprintf("%032x", 20) || page[0].SpanCount != 3 || page[0].Errors != 2 || strings.Join(page[0].Services, ",") != "api,db" || page[0].Name != "GET /orders" || page[0].DurationUS != 200000 {
		t.Fatalf("newest trace: %+v", page[0])
	}
	if page[1].SpanCount != 2 || page[1].Errors != 0 {
		t.Fatalf("even trace: %+v", page[1])
	}
	last := page[len(page)-1]
	rest, err := c.TraceList(ctx, clickhouse.SpanQuery{ProjectID: "p1", From: from, To: to, Limit: 15, BeforeTS: last.StartTS, BeforeID: last.TraceID})
	if err != nil || len(rest) != 5 || rest[0].TraceID != fmt.Sprintf("%032x", 5) {
		t.Fatalf("cursor page: %d %v %+v", len(rest), err, rest)
	}
	// Span-level filter: traces containing a failing client span.
	parsed, _ := logquery.Parse(`kind:client status:error`)
	where, args, _ := parsed.SQLFor(logquery.Spans)
	failed, err := c.TraceList(ctx, clickhouse.SpanQuery{ProjectID: "p1", From: from, To: to, Limit: 50, Where: where, Args: args})
	if err != nil || len(failed) != 10 || failed[0].Kind != "server" {
		t.Fatalf("filtered list: %d %v", len(failed), err)
	}
	parsed, _ = logquery.Parse(`duration:>150 "GET /orders"`)
	where, args, _ = parsed.SQLFor(logquery.Spans)
	slow, err := c.TraceList(ctx, clickhouse.SpanQuery{ProjectID: "p1", From: from, To: to, Limit: 50, Where: where, Args: args})
	if err != nil || len(slow) != 5 {
		t.Fatalf("slow roots: %d %v", len(slow), err)
	}

	// Stats: per name and the entry-span latency distribution.
	st, err := c.TraceStats(ctx, clickhouse.SpanQuery{ProjectID: "p1", From: from, To: to})
	if err != nil || len(st.ByName) != 3 {
		t.Fatalf("stats: %+v %v", st, err)
	}
	byName := map[string]int{}
	for i, s := range st.ByName {
		byName[s.Name] = i
	}
	orders := st.ByName[byName["GET /orders"]]
	if orders.Count != 20 || orders.ErrorPct != 50 || orders.P95US < 150000 || orders.P50US < 50000 {
		t.Fatalf("GET /orders stats: %+v", orders)
	}
	if inv := st.ByName[byName["GET inventory"]]; inv.Count != 10 || inv.ErrorPct != 100 {
		t.Fatalf("inventory stats: %+v", inv)
	}
	var total uint64
	for _, b := range st.Latency {
		if b.ToUS != b.FromUS*2 && b.FromUS != 0 {
			t.Fatalf("bucket edges: %+v", b)
		}
		total += b.Count
	}
	if total != 20 {
		t.Fatalf("latency histogram must cover the 20 entry spans: %d", total)
	}
}
