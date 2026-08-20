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

// escapeLike neutralizes LIKE wildcards in user search input so "%"/"_" in a
// search term match literally (values are already parameterized; this fixes
// match semantics, not injection).
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// searchWhere builds " WHERE (col::text ILIKE $n ESCAPE '\' OR ...)" plus its args.
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
		likes[i] = fmt.Sprintf("%s::text ILIKE $%d ESCAPE '\\'", q, start+i)
		args[i] = "%" + escapeLike(search) + "%"
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
	if len(cols) == 0 {
		return "", 0, fmt.Errorf("no columns to insert")
	}
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

// keyWhere builds the WHERE clause for one or more key columns:
// ` WHERE "a"=$n AND "b"=$n+1`. Key columns can be a composite key — the key
// set is chosen at definition time and used purely as the update/delete
// predicate, so it does not have to be the real Postgres PK.
func keyWhere(keyCols []string, start int) (string, error) {
	if len(keyCols) == 0 {
		return "", fmt.Errorf("no key columns")
	}
	qs, err := quoteAll(keyCols)
	if err != nil {
		return "", err
	}
	parts := make([]string, len(qs))
	for i, q := range qs {
		parts[i] = fmt.Sprintf("%s=$%d", q, start+i)
	}
	return " WHERE " + strings.Join(parts, " AND "), nil
}

func BuildUpdateByKey(schema, table string, setCols []string, keyCols []string) (string, int, error) {
	if len(setCols) == 0 {
		return "", 0, fmt.Errorf("no columns to update")
	}
	tbl, err := qualify(schema, table)
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
	kw, err := keyWhere(keyCols, len(setCols)+1)
	if err != nil {
		return "", 0, err
	}
	return "UPDATE " + tbl + " SET " + strings.Join(sets, ",") + kw, len(setCols), nil
}

func BuildDeleteByKey(schema, table string, keyCols []string) (string, error) {
	tbl, err := qualify(schema, table)
	if err != nil {
		return "", err
	}
	kw, err := keyWhere(keyCols, 1)
	if err != nil {
		return "", err
	}
	return "DELETE FROM " + tbl + kw, nil
}

func BuildFetchByKey(schema, table string, keyCols []string, columns []string) (string, error) {
	tbl, err := qualify(schema, table)
	if err != nil {
		return "", err
	}
	qs, err := quoteAll(columns)
	if err != nil {
		return "", err
	}
	kw, err := keyWhere(keyCols, 1)
	if err != nil {
		return "", err
	}
	return "SELECT " + strings.Join(qs, ",") + " FROM " + tbl + kw, nil
}

// BuildFetchByRefValues selects refCol + displayCols (deduped) from table
// where refCol IN ($1..$n). nValues must be > 0; the caller passes the args.
func BuildFetchByRefValues(schema, table, refCol string, displayCols []string, nValues int) (string, error) {
	if nValues <= 0 {
		return "", fmt.Errorf("no ref values to look up")
	}
	tbl, err := qualify(schema, table)
	if err != nil {
		return "", err
	}
	qr, err := QuoteIdent(refCol)
	if err != nil {
		return "", err
	}
	names := []string{refCol}
	for _, d := range displayCols {
		if d != refCol {
			names = append(names, d)
		}
	}
	qs, err := quoteAll(names)
	if err != nil {
		return "", err
	}
	ph := make([]string, nValues)
	for i := range ph {
		ph[i] = fmt.Sprintf("$%d", i+1)
	}
	return "SELECT " + strings.Join(qs, ",") + " FROM " + tbl +
		" WHERE " + qr + " IN (" + strings.Join(ph, ",") + ")", nil
}

// BuildCountByRefEq counts rows where col = $1 (delete-reference protection).
func BuildCountByRefEq(schema, table, col string) (string, error) {
	tbl, err := qualify(schema, table)
	if err != nil {
		return "", err
	}
	q, err := QuoteIdent(col)
	if err != nil {
		return "", err
	}
	return "SELECT COUNT(*) FROM " + tbl + " WHERE " + q + "=$1", nil
}
