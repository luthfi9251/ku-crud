package engine

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/luthfi9251/kucrud-core/defs"
)

// cnode is one AST node of a compiled computed formula.
// kind: 'n' number, 's' string literal, 'i' ident, 'o' binary op,
//
//	'u' unary minus, 'c' concat.
type cnode struct {
	kind byte
	num  float64
	s    string
	name string
	op   byte
	args []*cnode
}

// computedCols returns the definition's computed (virtual) columns.
func computedCols(cols []defs.Column) []defs.Column {
	var out []defs.Column
	for _, c := range cols {
		if c.IsComputed {
			out = append(out, c)
		}
	}
	return out
}

// ApplyComputed appends each computed column's value to every row (nil when
// the formula yields NULL or fails). Never touches the database.
func ApplyComputed(cols []defs.Column, rows []map[string]any) {
	comps := computedCols(cols)
	if len(comps) == 0 || len(rows) == 0 {
		return
	}
	evals := make([]func(map[string]any) any, len(comps))
	for i, c := range comps {
		_, fn, err := CompileComputed(c, cols)
		if err != nil {
			evals[i] = func(map[string]any) any { return nil } // invalid def → nil, never a request failure
			continue
		}
		evals[i] = fn
	}
	for _, r := range rows {
		for i, c := range comps {
			r[c.Name] = evals[i](r)
		}
	}
}

// CompileComputed parses and type-checks one computed formula against the
// definition's real (non-computed) columns. Returns the result field type
// ("number" or "text") and an evaluator.
func CompileComputed(c defs.Column, cols []defs.Column) (string, func(map[string]any) any, error) {
	p := &cparser{s: c.ComputedFormula, byName: map[string]defs.Column{}}
	for _, col := range cols {
		if !col.IsComputed {
			p.byName[col.Name] = col
		}
	}
	root, ft, err := p.parseExpr()
	if err != nil {
		return "", nil, err
	}
	if p.pos < len(p.s) {
		return "", nil, fmt.Errorf("unexpected token %q", p.s[p.pos:])
	}
	return ft, func(row map[string]any) any { return evalNode(root, row) }, nil
}

// --- tokenizer + recursive-descent parser -----------------------------------

type cparser struct {
	s      string
	pos    int
	byName map[string]defs.Column
}

func (p *cparser) skipWs() {
	for p.pos < len(p.s) && unicode.IsSpace(rune(p.s[p.pos])) {
		p.pos++
	}
}

func (p *cparser) peek() byte {
	p.skipWs()
	if p.pos >= len(p.s) {
		return 0
	}
	return p.s[p.pos]
}

// ident parses [A-Za-z_][A-Za-z0-9_]* at the cursor.
func (p *cparser) ident() (string, bool) {
	p.skipWs()
	start := p.pos
	if p.pos >= len(p.s) || !(isIdentStart(p.s[p.pos])) {
		return "", false
	}
	for p.pos < len(p.s) && isIdentPart(p.s[p.pos]) {
		p.pos++
	}
	return p.s[start:p.pos], true
}

func isIdentStart(b byte) bool { return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isIdentPart(b byte) bool  { return isIdentStart(b) || (b >= '0' && b <= '9') }

func (p *cparser) number() (float64, bool) {
	p.skipWs()
	start := p.pos
	hasDot := false
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c >= '0' && c <= '9' {
			p.pos++
		} else if c == '.' && !hasDot {
			hasDot = true
			p.pos++
		} else {
			break
		}
	}
	if p.pos == start {
		return 0, false
	}
	v, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	return v, err == nil
}

// stringLit parses a double-quoted literal with \" escapes.
func (p *cparser) stringLit() (string, bool) {
	p.skipWs()
	if p.pos >= len(p.s) || p.s[p.pos] != '"' {
		return "", false
	}
	p.pos++
	var b strings.Builder
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		p.pos++
		if c == '\\' && p.pos < len(p.s) {
			b.WriteByte(p.s[p.pos])
			p.pos++
			continue
		}
		if c == '"' {
			return b.String(), true
		}
		b.WriteByte(c)
	}
	return "", false
}

func (p *cparser) expect(ch byte) bool {
	p.skipWs()
	if p.pos < len(p.s) && p.s[p.pos] == ch {
		p.pos++
		return true
	}
	return false
}

// expr   := term (('+'|'-') term)*        — float, or a single text term
func (p *cparser) parseExpr() (*cnode, string, error) {
	left, ft, err := p.parseTerm()
	if err != nil {
		return nil, "", err
	}
	for {
		op := p.peek()
		if op != '+' && op != '-' {
			return left, ft, nil
		}
		p.pos++
		right, rft, err := p.parseTerm()
		if err != nil {
			return nil, "", err
		}
		if ft != "number" || rft != "number" {
			return nil, "", fmt.Errorf("arithmetic requires number operands")
		}
		left = &cnode{kind: 'o', op: op, args: []*cnode{left, right}}
	}
}

// term   := factor (('*'|'/') factor)*    — float
func (p *cparser) parseTerm() (*cnode, string, error) {
	left, ft, err := p.parseFactor()
	if err != nil {
		return nil, "", err
	}
	for {
		op := p.peek()
		if op != '*' && op != '/' {
			return left, ft, nil
		}
		p.pos++
		right, rft, err := p.parseFactor()
		if err != nil {
			return nil, "", err
		}
		if ft != "number" || rft != "number" {
			return nil, "", fmt.Errorf("arithmetic requires number operands")
		}
		left = &cnode{kind: 'o', op: op, args: []*cnode{left, right}}
		ft = "number"
	}
}

// factor := '-' factor | number | ident | '(' expr ')' | CONCAT '(' ... ')'
func (p *cparser) parseFactor() (*cnode, string, error) {
	p.skipWs()
	if p.pos >= len(p.s) {
		return nil, "", fmt.Errorf("unexpected end of formula")
	}
	if p.s[p.pos] == '-' {
		p.pos++
		inner, ft, err := p.parseFactor()
		if err != nil {
			return nil, "", err
		}
		if ft != "number" {
			return nil, "", fmt.Errorf("unary minus requires a number operand")
		}
		return &cnode{kind: 'u', args: []*cnode{inner}}, "number", nil
	}
	if n, ok := p.number(); ok {
		return &cnode{kind: 'n', num: n}, "number", nil
	}
	if s, ok := p.stringLit(); ok {
		return &cnode{kind: 's', s: s}, "text", nil
	}
	if id, ok := p.ident(); ok {
		if id == "CONCAT" {
			return p.parseConcat()
		}
		col, ok := p.byName[id]
		if !ok {
			return nil, "", fmt.Errorf("unknown column %q", id)
		}
		if col.FieldType == "fk" {
			return nil, "", fmt.Errorf("column %q cannot be used in a formula", id)
		}
		ft := "number"
		if col.FieldType == "text" {
			ft = "text"
		}
		return &cnode{kind: 'i', name: id}, ft, nil
	}
	if p.expect('(') {
		inner, ft, err := p.parseExpr()
		if err != nil {
			return nil, "", err
		}
		if !p.expect(')') {
			return nil, "", fmt.Errorf("missing closing paren")
		}
		return inner, ft, nil
	}
	return nil, "", fmt.Errorf("unexpected token %q", p.s[p.pos])
}

// concat := 'CONCAT' '(' arg (',' arg)+ ')'   — text; idents must be text columns
func (p *cparser) parseConcat() (*cnode, string, error) {
	if !p.expect('(') {
		return nil, "", fmt.Errorf("CONCAT needs (")
	}
	node := &cnode{kind: 'c'}
	for {
		p.skipWs()
		if s, ok := p.stringLit(); ok {
			node.args = append(node.args, &cnode{kind: 's', s: s})
		} else if id, ok := p.ident(); ok {
			col, ok := p.byName[id]
			if !ok {
				return nil, "", fmt.Errorf("unknown column %q", id)
			}
			if col.FieldType != "text" {
				return nil, "", fmt.Errorf("CONCAT operand %q must be a text column", id)
			}
			node.args = append(node.args, &cnode{kind: 'i', name: id})
		} else {
			return nil, "", fmt.Errorf("CONCAT operands must be text columns or string literals")
		}
		if !p.expect(',') {
			break
		}
	}
	if !p.expect(')') {
		return nil, "", fmt.Errorf("CONCAT missing closing paren")
	}
	if len(node.args) < 2 {
		return nil, "", fmt.Errorf("CONCAT needs at least 2 operands")
	}
	return node, "text", nil
}

// --- evaluation ---------------------------------------------------------------

// rowNum extracts a numeric value from a scanned row cell (nil-safe).
func rowNum(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, false
		}
		return n, true
	case []byte:
		n, err := strconv.ParseFloat(strings.TrimSpace(string(x)), 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func evalNode(n *cnode, row map[string]any) any {
	switch n.kind {
	case 'n':
		return n.num
	case 's':
		return n.s
	case 'i':
		v, ok := row[n.name]
		if !ok || v == nil {
			return nil
		}
		return v
	case 'u':
		v := evalNode(n.args[0], row)
		if v == nil {
			return nil
		}
		if f, ok := rowNum(v); ok {
			return -f
		}
		return nil
	case 'o':
		l, r := evalNode(n.args[0], row), evalNode(n.args[1], row)
		if l == nil || r == nil {
			return nil
		}
		lf, lok := rowNum(l)
		rf, rok := rowNum(r)
		if !lok || !rok {
			return nil
		}
		switch n.op {
		case '+':
			return lf + rf
		case '-':
			return lf - rf
		case '*':
			return lf * rf
		case '/':
			if rf == 0 {
				return nil // division by zero → NULL, not an error
			}
			return lf / rf
		}
		return nil
	case 'c':
		var b strings.Builder
		for _, a := range n.args {
			v := evalNode(a, row)
			if v == nil {
				return nil // NULL operand → NULL result
			}
			if s, ok := v.(string); ok {
				b.WriteString(s)
			} else {
				return nil
			}
		}
		return b.String()
	}
	return nil
}
