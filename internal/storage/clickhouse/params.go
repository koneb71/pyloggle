package clickhouse

import (
	"strconv"
	"strings"
	"time"
)

// ClickHouse "query parameters" (the `{name:Type}` syntax) are passed as strings over the wire
// and parsed server-side into the declared Type. The clickhouse-go driver enforces this.

func paramUInt32(n int) string {
	if n < 0 {
		n = 0
	}
	return strconv.FormatUint(uint64(n), 10)
}

func paramInt32(n int) string {
	return strconv.FormatInt(int64(n), 10)
}

func paramDateTime64Millis(t time.Time) string {
	// Matches DateTime64(3) expectations: "YYYY-MM-DD HH:MM:SS.mmm"
	// ClickHouse parses this into the declared timezone on the column/expression.
	return paramStringLiteral(t.UTC().Format("2006-01-02 15:04:05.000"))
}

func paramStringLiteral(s string) string {
	// ClickHouse query parameters are substituted as expressions. For String-typed parameters,
	// ClickHouse expects a quoted string literal.
	//
	// Escape: single quote is escaped by backslash in ClickHouse.
	esc := strings.ReplaceAll(s, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `'`, `\'`)
	return "'" + esc + "'"
}


