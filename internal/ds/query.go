package ds

import (
	"fmt"
	"regexp"
	"strings"
)

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// QuoteIdent is the ONLY way identifiers reach SQL. Strict allowlist beats
// escaping: introspected names either match or the definition is rejected.
func QuoteIdent(s string) (string, error) {
	if !identRe.MatchString(s) {
		return "", fmt.Errorf("invalid identifier %q", s)
	}
	return `"` + s + `"`, nil
}

func qualify(schema, table string) (string, error) {
	qs, err := QuoteIdent(schema)
	if err != nil {
		return "", err
	}
	qt, err := QuoteIdent(table)
	if err != nil {
		return "", err
	}
	return qs + "." + qt, nil
}

func quoteAll(cols []string) ([]string, error) {
	out := make([]string, len(cols))
	for i, c := range cols {
		q, err := QuoteIdent(c)
		if err != nil {
			return nil, err
		}
		out[i] = q
	}
	return out, nil
}

type ListParams struct {
	Schema, Table    string
	Columns          []string
	Searchable       []string
	Search           string
	SortCol, SortDir string
	Limit, Offset    int
}

// searchWhere builds " WHERE (col::text ILIKE $n OR ...)" plus its args.
// One placeholder per searchable column (Postgres allows re-using $n, but
// distinct numbering keeps arg order trivially aligned).
func searchWhere(searchable []string, search string, start int) (string, []any, int) {
	if search == "" || len(searchable) == 0 {
		return "", nil, start
	}
	qs, err := quoteAll(searchable)
	if err != nil {
		return "", nil, start // unreachable: handler pre-validates
	}
	likes := make([]string, len(qs))
	args := make([]any, len(qs))
	for i, q := range qs {
		likes[i] = fmt.Sprintf("%s::text ILIKE $%d", q, start+i)
		args[i] = "%" + search + "%"
	}
	return " WHERE (" + strings.Join(likes, " OR ") + ")", args, start + len(qs)
}

func BuildList(p ListParams) (string, []any, error) {
	tbl, err := qualify(p.Schema, p.Table)
	if err != nil {
		return "", nil, err
	}
	cols, err := quoteAll(p.Columns)
	if err != nil {
		return "", nil, err
	}
	valid := map[string]bool{}
	for _, c := range p.Columns {
		valid[c] = true
	}
	if !valid[p.SortCol] {
		return "", nil, fmt.Errorf("sort column %q not selectable", p.SortCol)
	}
	qsort, err := QuoteIdent(p.SortCol)
	if err != nil {
		return "", nil, err
	}
	if p.SortDir != "ASC" && p.SortDir != "DESC" {
		return "", nil, fmt.Errorf("invalid sort direction %q", p.SortDir)
	}
	where, args, next := searchWhere(p.Searchable, p.Search, 1)
	sql := "SELECT " + strings.Join(cols, ",") + " FROM " + tbl + where +
		" ORDER BY " + qsort + " " + p.SortDir +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d", next, next+1)
	args = append(args, p.Limit, p.Offset)
	return sql, args, nil
}

func BuildCount(p ListParams) (string, []any, error) {
	tbl, err := qualify(p.Schema, p.Table)
	if err != nil {
		return "", nil, err
	}
	where, args, _ := searchWhere(p.Searchable, p.Search, 1)
	return "SELECT COUNT(*) FROM " + tbl + where, args, nil
}

func BuildInsert(schema, table string, cols []string) (string, int, error) {
	tbl, err := qualify(schema, table)
	if err != nil {
		return "", 0, err
	}
	qs, err := quoteAll(cols)
	if err != nil {
		return "", 0, err
	}
	ph := make([]string, len(cols))
	for i := range cols {
		ph[i] = fmt.Sprintf("$%d", i+1)
	}
	return "INSERT INTO " + tbl + " (" + strings.Join(qs, ",") + ") VALUES (" +
		strings.Join(ph, ",") + ")", len(cols), nil
}

func BuildUpdateByPK(schema, table, pk string, setCols []string) (string, int, error) {
	tbl, err := qualify(schema, table)
	if err != nil {
		return "", 0, err
	}
	qpk, err := QuoteIdent(pk)
	if err != nil {
		return "", 0, err
	}
	qs, err := quoteAll(setCols)
	if err != nil {
		return "", 0, err
	}
	sets := make([]string, len(qs))
	for i, q := range qs {
		sets[i] = fmt.Sprintf("%s=$%d", q, i+1)
	}
	return "UPDATE " + tbl + " SET " + strings.Join(sets, ",") +
		fmt.Sprintf(" WHERE %s=$%d", qpk, len(setCols)+1), len(setCols), nil
}

func BuildDeleteByPK(schema, table, pk string) (string, error) {
	tbl, err := qualify(schema, table)
	if err != nil {
		return "", err
	}
	qpk, err := QuoteIdent(pk)
	if err != nil {
		return "", err
	}
	return "DELETE FROM " + tbl + fmt.Sprintf(" WHERE %s=$1", qpk), nil
}

func BuildFetchByPK(schema, table, pk string, columns []string) (string, error) {
	tbl, err := qualify(schema, table)
	if err != nil {
		return "", err
	}
	qpk, err := QuoteIdent(pk)
	if err != nil {
		return "", err
	}
	qs, err := quoteAll(columns)
	if err != nil {
		return "", err
	}
	return "SELECT " + strings.Join(qs, ",") + " FROM " + tbl +
		fmt.Sprintf(" WHERE %s=$1", qpk), nil
}
