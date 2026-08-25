package defs

type SortDir string

const (
	Asc  SortDir = "asc"
	Desc SortDir = "desc"
)

type Table struct {
	Name            string // route name, also the menu key (was Label)
	Label           string
	Description     string
	Schema, PhysTab string // physical table (== Name for simple cases)
	Keys            []string
	PageSize        int
	DefaultSortCol  string
	DefaultSortDir  string
	DefaultView     string
	ViewConfig      string
	SourceType      string // "table" | "query"
	QuerySQL        string
	Hooks           string // assignments JSON (contract unchanged)
	Actions         string // actions JSON (contract unchanged)
	Columns         []Column
}

type Column struct {
	Name, Label, FieldType                            string
	EnumOptions                                       []string
	Editable, Required, Visible, Searchable, Sortable bool
	Position                                          int
	Validations                                       []ValidationRule
	Formatting                                        string
	IsComputed                                        bool
	ComputedFormula                                   string
	BaseType                                          string
	FK                                                *FK
	M2M                                               *M2M
}

// FK references another *definition name*, not an int64 def id.
type FK struct {
	Table          string // target definition name; "" = self
	RefColumn      string
	DisplayColumns []string
}

// M2M references the junction by table name, not junction def id.
type M2M struct {
	JunctionTable  string
	SrcCol, TgtCol string
	DisplayColumns []string
}

type ValidationRule struct {
	Type  string // email | min_len | max_len | number | text
	Param int
}
