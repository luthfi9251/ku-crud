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

// escapeLike neutralizes LIKE wildcards in user search input so "%"/"_" in a
// search term match literally (values are already parameterized; this fixes
// match semantics, not injection).
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// sqlDialect parameterizes the generic SQL builders for one database
// family. Adding a new SQL adapter = new dialect value (+ adapter file).
type sqlDialect struct {
	identQuote  byte
	placeholder func(n int) string // 1-based arg position
	searchExpr  func(quoted string) string
	likeOp      string
	likeEscape  string // ESCAPE literal as it must appear in SQL text
}

var pgDialect = sqlDialect{
	identQuote:  '"',
	placeholder: func(n int) string { return fmt.Sprintf("$%d", n) },
	searchExpr:  func(q string) string { return q + "::text" },
	likeOp:      "ILIKE",
	likeEscape:  `'\'`,
}

var mysqlDialect = sqlDialect{
	identQuote:  '`',
	placeholder: func(int) string { return "?" },
	searchExpr:  func(q string) string { return "CAST(" + q + " AS CHAR)" },
	likeOp:      "LIKE",
	likeEscape:  `'\\'`,
}

func (dt sqlDialect) quoteIdent(s string) (string, error) {
	if !identRe.MatchString(s) {
		return "", fmt.Errorf("invalid identifier %q", s)
	}
	return string(dt.identQuote) + s + string(dt.identQuote), nil
}

func (dt sqlDialect) qualify(schema, table string) (string, error) {
	qs, err := dt.quoteIdent(schema)
	if err != nil {
		return "", err
	}
	qt, err := dt.quoteIdent(table)
	if err != nil {
		return "", err
	}
	return qs + "." + qt, nil
}

func (dt sqlDialect) quoteAll(cols []string) ([]string, error) {
	out := make([]string, len(cols))
	for i, c := range cols {
		q, err := dt.quoteIdent(c)
		if err != nil {
			return nil, err
		}
		out[i] = q
	}
	return out, nil
}

// searchWhere builds " WHERE (expr LIKE $n ESCAPE '\' OR ...)" plus args.
func (dt sqlDialect) searchWhere(searchable []string, search string, start int) (string, []any, int) {
	if search == "" || len(searchable) == 0 {
		return "", nil, start
	}
	qs, err := dt.quoteAll(searchable)
	if err != nil {
		return "", nil, start // unreachable: handler pre-validates
	}
	likes := make([]string, len(qs))
	args := make([]any, len(qs))
	for i, q := range qs {
		likes[i] = fmt.Sprintf("%s %s %s ESCAPE %s", dt.searchExpr(q), dt.likeOp, dt.placeholder(start+i), dt.likeEscape)
		args[i] = "%" + escapeLike(search) + "%"
	}
	return " WHERE (" + strings.Join(likes, " OR ") + ")", args, start + len(qs)
}

func (dt sqlDialect) buildList(p ListParams) (string, []any, error) {
	tbl, err := dt.qualify(p.Schema, p.Table)
	if err != nil {
		return "", nil, err
	}
	cols, err := dt.quoteAll(p.Columns)
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
	qsort, err := dt.quoteIdent(p.SortCol)
	if err != nil {
		return "", nil, err
	}
	if p.SortDir != "ASC" && p.SortDir != "DESC" {
		return "", nil, fmt.Errorf("invalid sort direction %q", p.SortDir)
	}
	where, args, next := dt.searchWhere(p.Searchable, p.Search, 1)
	sql := "SELECT " + strings.Join(cols, ",") + " FROM " + tbl + where +
		" ORDER BY " + qsort + " " + p.SortDir +
		fmt.Sprintf(" LIMIT %s OFFSET %s", dt.placeholder(next), dt.placeholder(next+1))
	args = append(args, p.Limit, p.Offset)
	return sql, args, nil
}

func (dt sqlDialect) buildCount(p ListParams) (string, []any, error) {
	tbl, err := dt.qualify(p.Schema, p.Table)
	if err != nil {
		return "", nil, err
	}
	where, args, _ := dt.searchWhere(p.Searchable, p.Search, 1)
	return "SELECT COUNT(*) FROM " + tbl + where, args, nil
}

func (dt sqlDialect) buildInsert(schema, table string, cols []string, vals []any) (string, error) {
	if len(cols) == 0 {
		return "", fmt.Errorf("no columns to insert")
	}
	if len(cols) != len(vals) {
		return "", fmt.Errorf("insert columns (%d) and values (%d) mismatch", len(cols), len(vals))
	}
	tbl, err := dt.qualify(schema, table)
	if err != nil {
		return "", err
	}
	qs, err := dt.quoteAll(cols)
	if err != nil {
		return "", err
	}
	ph := make([]string, len(cols))
	for i := range cols {
		ph[i] = dt.placeholder(i + 1)
	}
	return "INSERT INTO " + tbl + " (" + strings.Join(qs, ",") + ") VALUES (" +
		strings.Join(ph, ",") + ")", nil
}

func (dt sqlDialect) keyWhere(keyCols []string, start int) (string, error) {
	if len(keyCols) == 0 {
		return "", fmt.Errorf("no key columns")
	}
	qs, err := dt.quoteAll(keyCols)
	if err != nil {
		return "", err
	}
	parts := make([]string, len(qs))
	for i, q := range qs {
		parts[i] = fmt.Sprintf("%s=%s", q, dt.placeholder(start+i))
	}
	return " WHERE " + strings.Join(parts, " AND "), nil
}

func (dt sqlDialect) buildUpdateByKey(schema, table string, setCols []string, setVals []any, keyCols []string, keyVals []any) (string, []any, error) {
	if len(setCols) == 0 {
		return "", nil, fmt.Errorf("no columns to update")
	}
	if len(setCols) != len(setVals) || len(keyCols) != len(keyVals) {
		return "", nil, fmt.Errorf("update column/value mismatch")
	}
	tbl, err := dt.qualify(schema, table)
	if err != nil {
		return "", nil, err
	}
	qs, err := dt.quoteAll(setCols)
	if err != nil {
		return "", nil, err
	}
	sets := make([]string, len(qs))
	for i, q := range qs {
		sets[i] = fmt.Sprintf("%s=%s", q, dt.placeholder(i+1))
	}
	kw, err := dt.keyWhere(keyCols, len(setCols)+1)
	if err != nil {
		return "", nil, err
	}
	return "UPDATE " + tbl + " SET " + strings.Join(sets, ",") + kw, append(append([]any{}, setVals...), keyVals...), nil
}

func (dt sqlDialect) buildDeleteByKey(schema, table string, keyCols []string, keyVals []any) (string, []any, error) {
	if len(keyCols) != len(keyVals) || len(keyCols) == 0 {
		return "", nil, fmt.Errorf("delete key/value mismatch")
	}
	tbl, err := dt.qualify(schema, table)
	if err != nil {
		return "", nil, err
	}
	kw, err := dt.keyWhere(keyCols, 1)
	if err != nil {
		return "", nil, err
	}
	return "DELETE FROM " + tbl + kw, append([]any{}, keyVals...), nil
}

func (dt sqlDialect) buildFetchByKey(schema, table string, keyCols []string, keyVals []any, cols []string) (string, []any, error) {
	if len(keyCols) != len(keyVals) || len(keyCols) == 0 {
		return "", nil, fmt.Errorf("fetch key/value mismatch")
	}
	tbl, err := dt.qualify(schema, table)
	if err != nil {
		return "", nil, err
	}
	qs, err := dt.quoteAll(cols)
	if err != nil {
		return "", nil, err
	}
	kw, err := dt.keyWhere(keyCols, 1)
	if err != nil {
		return "", nil, err
	}
	return "SELECT " + strings.Join(qs, ",") + " FROM " + tbl + kw, append([]any{}, keyVals...), nil
}

// buildFetchByRefValues selects refCol + displayCols (deduped) where
// refCol IN (...vals). vals must be non-empty.
func (dt sqlDialect) buildFetchByRefValues(schema, table, refCol string, displayCols []string, vals []any) (string, []any, error) {
	if len(vals) == 0 {
		return "", nil, fmt.Errorf("no ref values to look up")
	}
	tbl, err := dt.qualify(schema, table)
	if err != nil {
		return "", nil, err
	}
	qr, err := dt.quoteIdent(refCol)
	if err != nil {
		return "", nil, err
	}
	names := []string{refCol}
	for _, d := range displayCols {
		if d != refCol {
			names = append(names, d)
		}
	}
	qs, err := dt.quoteAll(names)
	if err != nil {
		return "", nil, err
	}
	ph := make([]string, len(vals))
	args := make([]any, len(vals))
	for i, v := range vals {
		ph[i] = dt.placeholder(i + 1)
		args[i] = v
	}
	return "SELECT " + strings.Join(qs, ",") + " FROM " + tbl +
		" WHERE " + qr + " IN (" + strings.Join(ph, ",") + ")", args, nil
}

func (dt sqlDialect) buildCountByRefEq(schema, table, col string, val any) (string, []any, error) {
	tbl, err := dt.qualify(schema, table)
	if err != nil {
		return "", nil, err
	}
	q, err := dt.quoteIdent(col)
	if err != nil {
		return "", nil, err
	}
	return "SELECT COUNT(*) FROM " + tbl + " WHERE " + q + "=" + dt.placeholder(1), []any{val}, nil
}

// buildFetchPairs selects col + retCol where col IN (...vals) — the junction
// link lookup for many-to-many relations.
func (dt sqlDialect) buildFetchPairs(schema, table, col, retCol string, vals []any) (string, []any, error) {
	if len(vals) == 0 {
		return "", nil, fmt.Errorf("no pair values to look up")
	}
	tbl, err := dt.qualify(schema, table)
	if err != nil {
		return "", nil, err
	}
	qc, err := dt.quoteIdent(col)
	if err != nil {
		return "", nil, err
	}
	qr, err := dt.quoteIdent(retCol)
	if err != nil {
		return "", nil, err
	}
	ph := make([]string, len(vals))
	args := make([]any, len(vals))
	for i, v := range vals {
		ph[i] = dt.placeholder(i + 1)
		args[i] = v
	}
	return "SELECT " + qc + "," + qr + " FROM " + tbl +
		" WHERE " + qc + " IN (" + strings.Join(ph, ",") + ")", args, nil
}

// buildDeletePairs deletes junction rows matching one (col1, col2) pair.
func (dt sqlDialect) buildDeletePairs(schema, table, col1, col2 string) (string, []any, error) {
	tbl, err := dt.qualify(schema, table)
	if err != nil {
		return "", nil, err
	}
	q1, err := dt.quoteIdent(col1)
	if err != nil {
		return "", nil, err
	}
	q2, err := dt.quoteIdent(col2)
	if err != nil {
		return "", nil, err
	}
	return "DELETE FROM " + tbl + " WHERE " + q1 + "=" + dt.placeholder(1) +
		" AND " + q2 + "=" + dt.placeholder(2), nil, nil
}

// scanTargets/deref/rowToMap: generic database/sql scanning for adapters.
func scanTargets(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = new(any)
	}
	return out
}

func deref(scan []any) []any {
	out := make([]any, len(scan))
	for i, p := range scan {
		v := p.(*any)
		out[i] = *v
	}
	return out
}

func rowToMap(cols []string, scan []any) map[string]any {
	m := make(map[string]any, len(cols))
	for i, c := range cols {
		v := scan[i]
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		m[c] = v
	}
	return m
}
