package ds

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryAlias names the derived table wrapping a stored query view.
const queryAlias = "ku_q"

// QueryTimeout bounds one query-view execution (layer-3 guard).
var QueryTimeout = 15 * time.Second

// IsQueryTimeout reports whether err is a driver-level statement timeout.
func IsQueryTimeout(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "57014" // query_canceled (statement_timeout)
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 3024 || me.Number == 1969 // query / statement timeout
	}
	return false
}

// QueryParams carries one query-view list/count request. Columns, Searchable
// and SortCol resolve ONLY from stored metadata (validated by the api layer).
type QueryParams struct {
	Query      string
	Columns    []string
	Searchable []string
	Search     string
	SortCol    string
	SortDir    string
	Limit      int
	Offset     int
	Filters    []ColumnFilter
}

func (dt sqlDialect) buildQueryList(p QueryParams) (string, []any, error) {
	for _, f := range p.Filters {
		if f.Join != nil {
			return "", nil, fmt.Errorf("fk join filters are not supported on query views")
		}
	}
	// queryAlias is a fixed allowlisted identifier, rendered unquoted by design.
	alias := queryAlias
	cols := make([]string, len(p.Columns))
	for i, c := range p.Columns {
		q, err := dt.colRef(alias, c)
		if err != nil {
			return "", nil, err
		}
		cols[i] = q
	}
	valid := map[string]bool{}
	for _, c := range p.Columns {
		valid[c] = true
	}
	if !valid[p.SortCol] {
		return "", nil, fmt.Errorf("sort column %q not selectable", p.SortCol)
	}
	qsort, err := dt.colRef(alias, p.SortCol)
	if err != nil {
		return "", nil, err
	}
	if p.SortDir != "ASC" && p.SortDir != "DESC" {
		return "", nil, fmt.Errorf("invalid sort direction %q", p.SortDir)
	}
	sCond, sArgs, next := dt.searchWhere(p.Searchable, p.Search, 1, alias)
	joins, fCond, fArgs, next2, err := dt.filterParts(p.Filters, next, alias)
	if err != nil {
		return "", nil, err
	}
	var conds []string
	if sCond != "" {
		conds = append(conds, sCond)
	}
	if fCond != "" {
		conds = append(conds, fCond)
	}
	whereAll := ""
	if len(conds) > 0 {
		whereAll = " WHERE " + strings.Join(conds, " AND ")
	}
	args := append(sArgs, fArgs...)
	sql := "SELECT " + strings.Join(cols, ",") + " FROM (" + p.Query + ") " + alias +
		joins + whereAll + " ORDER BY " + qsort + " " + p.SortDir +
		fmt.Sprintf(" LIMIT %s OFFSET %s", dt.placeholder(next2), dt.placeholder(next2+1))
	args = append(args, p.Limit, p.Offset)
	return sql, args, nil
}

func (dt sqlDialect) buildQueryCount(p QueryParams) (string, []any, error) {
	for _, f := range p.Filters {
		if f.Join != nil {
			return "", nil, fmt.Errorf("fk join filters are not supported on query views")
		}
	}
	// queryAlias is a fixed allowlisted identifier, rendered unquoted by design.
	alias := queryAlias
	sCond, sArgs, next := dt.searchWhere(p.Searchable, p.Search, 1, alias)
	_, fCond, fArgs, _, err := dt.filterParts(p.Filters, next, alias)
	if err != nil {
		return "", nil, err
	}
	var conds []string
	if sCond != "" {
		conds = append(conds, sCond)
	}
	if fCond != "" {
		conds = append(conds, fCond)
	}
	whereAll := ""
	if len(conds) > 0 {
		whereAll = " WHERE " + strings.Join(conds, " AND ")
	}
	return "SELECT COUNT(*) FROM (" + p.Query + ") " + alias + whereAll, append(sArgs, fArgs...), nil
}
