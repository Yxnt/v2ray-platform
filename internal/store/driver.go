// Package store — simple-protocol driver wrapper for lib/pq.
//
// Neon.tech and PgBouncer in transaction mode do not support the PostgreSQL
// extended query protocol (Prepare / Execute). lib/pq always uses extended
// protocol for parameterised queries, causing
//   "pq: unnamed prepared statement does not exist (26000)"
//
// This file registers a "postgres-simple" sql driver that wraps lib/pq but
// formats parameter placeholders ($1, $2, …) inline before sending, so the
// actual query reaches the server with no arguments — forcing lib/pq to use
// the simple query protocol.
//
// String values are escaped with pq.QuoteLiteral so the approach is safe
// against SQL injection for all types we use.
package store

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

func init() {
	sql.Register("postgres-simple", &simplePQDriver{inner: &pq.Driver{}})
}

// simplePQDriver wraps lib/pq's driver and returns simplePQConn connections.
type simplePQDriver struct{ inner driver.Driver }

func (d *simplePQDriver) Open(name string) (driver.Conn, error) {
	c, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &simplePQConn{Conn: c}, nil
}

// simplePQConn wraps a lib/pq connection.
// It deliberately does NOT expose ExecerContext / QueryerContext so that
// database/sql always goes through our Prepare() override.
type simplePQConn struct{ driver.Conn }

// Prepare returns a fake statement that will format args inline at execution
// time and then run via lib/pq's simple query path (0 args → simple protocol).
func (c *simplePQConn) Prepare(query string) (driver.Stmt, error) {
	return &simpleStmt{conn: c.Conn, query: query}, nil
}

// Begin / Close are forwarded to the embedded driver.Conn automatically.

// ── Stmt ──────────────────────────────────────────────────────────────────

type simpleStmt struct {
	conn  driver.Conn
	query string
}

func (s *simpleStmt) Close() error { return nil }

// NumInput returns -1 so database/sql does not validate argument count.
func (s *simpleStmt) NumInput() int { return -1 }

func (s *simpleStmt) Exec(args []driver.Value) (driver.Result, error) {
	q := inlineArgs(s.query, args)
	if e, ok := s.conn.(driver.Execer); ok {
		return e.Exec(q, nil)
	}
	// fallback: prepare+exec on underlying conn
	st, err := s.conn.Prepare(q)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	return st.Exec(nil)
}

func (s *simpleStmt) Query(args []driver.Value) (driver.Rows, error) {
	q := inlineArgs(s.query, args)
	if qr, ok := s.conn.(driver.Queryer); ok {
		return qr.Query(q, nil)
	}
	// fallback: prepare+query on underlying conn
	st, err := s.conn.Prepare(q)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	return st.Query(nil)
}

// ── arg formatting ─────────────────────────────────────────────────────────

var placeholderRE = regexp.MustCompile(`\$(\d+)`)

// inlineArgs replaces $1 … $N placeholders with safely-escaped literal values.
func inlineArgs(query string, args []driver.Value) string {
	if len(args) == 0 {
		return query
	}
	return placeholderRE.ReplaceAllStringFunc(query, func(match string) string {
		n, err := strconv.Atoi(match[1:])
		if err != nil || n < 1 || n > len(args) {
			return match
		}
		return fmtValue(args[n-1])
	})
}

func fmtValue(v driver.Value) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'g', -1, 64)
	case bool:
		if val {
			return "TRUE"
		}
		return "FALSE"
	case []byte:
		return pq.QuoteLiteral(string(val))
	case string:
		return pq.QuoteLiteral(val)
	case time.Time:
		// RFC3339Nano quoted, cast to timestamptz so Postgres parses it.
		return pq.QuoteLiteral(val.UTC().Format(time.RFC3339Nano)) + "::timestamptz"
	default:
		return pq.QuoteLiteral(fmt.Sprintf("%v", val))
	}
}

// formatQueryForLog returns the query with args inlined — useful for debug logging.
func formatQueryForLog(query string, args []interface{}) string {
	dvals := make([]driver.Value, len(args))
	for i, a := range args {
		if dv, ok := a.(driver.Value); ok {
			dvals[i] = dv
		} else {
			dvals[i] = fmt.Sprintf("%v", a)
		}
	}
	return inlineArgs(query, dvals)
}

// quoteIdent is a thin alias so callers in postgres.go can use it if needed.
func quoteIdent(s string) string { return pq.QuoteIdentifier(s) }

// suppress unused warning
var _ = strings.Contains
