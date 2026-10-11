package clickhouse

import (
	"strings"
	"testing"
	"time"
)

func TestTrimDays(t *testing.T) {
	today := time.Date(2026, 10, 11, 9, 30, 0, 0, time.UTC)
	days := trimDays(today, 30, 7, time.Time{})
	if len(days) != 23 || days[0].Format("2006-01-02") != "2026-09-12" || days[len(days)-1].Format("2006-01-02") != "2026-10-04" {
		t.Fatalf("30 server / 7 project: %d days %v..%v", len(days), days[0], days[len(days)-1])
	}
	days = trimDays(today, 30, 7, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))
	if len(days) != 2 || days[0].Day() != 3 || days[1].Day() != 4 {
		t.Fatalf("resume after the last trimmed day: %v", days)
	}
	if trimDays(today, 30, 30, time.Time{}) != nil || trimDays(today, 30, 45, time.Time{}) != nil || trimDays(today, 30, 0, time.Time{}) != nil {
		t.Fatal("a project retention at or above the server's trims nothing")
	}
	if got := trimDays(today, 7, 1, time.Time{}); len(got) != 6 || got[0].Format("2006-01-02") != "2026-10-05" {
		t.Fatalf("7 server / 1 project: %v", got)
	}
}

func TestUsageExpressionsStayInSync(t *testing.T) {
	// Every source renders a self-contained INSERT ... SELECT and names a managed table.
	for _, s := range usageSources {
		sql := s.selectSQL("")
		if !strings.Contains(sql, "'"+s.Source+"' AS source") || !strings.Contains(sql, "FROM "+s.Table) || strings.Contains(sql, "?") {
			t.Fatalf("usage select for %s: %s", s.Source, sql)
		}
	}
	if r := UsageRatio(map[string]float64{"logs_raw": 0.1}, "logs"); r != 0.1 {
		t.Fatalf("ratio lookup: %v", r)
	}
	if r := UsageRatio(nil, "spans"); r != 0.25 {
		t.Fatalf("default ratio: %v", r)
	}
}
