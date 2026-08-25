package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"ku-crud/internal/defs"
	"ku-crud/internal/ds"
	"ku-crud/internal/engine"
	"ku-crud/internal/hooks"
	"ku-crud/internal/meta"
)

// tableDefInput accepts masked datasource ids from the client.
type tableDefInput struct {
	DatasourceID   string          `json:"datasourceId"`
	SchemaName     string          `json:"schemaName"`
	TableName      string          `json:"tableName"`
	Label          string          `json:"label"`
	Description    string          `json:"description"`
	KeyColumns     []string        `json:"keyColumns"`
	PageSize       int             `json:"pageSize"`
	DefaultSortCol string          `json:"defaultSortCol"`
	DefaultSortDir string          `json:"defaultSortDir"`
	DefaultView    string          `json:"defaultView"`
	ViewConfig     json.RawMessage `json:"viewConfig"`
	Hooks          json.RawMessage `json:"hooks"`
	Actions        json.RawMessage `json:"actions"`
	SourceType     string          `json:"sourceType"`
	QuerySQL       string          `json:"querySql"`
	Columns        []columnInput   `json:"columns"`
}

// columnInput mirrors meta.ColumnDef but takes the fk/m2m targets as masked
// tokens (fk target may also be the literal "self").
type columnInput struct {
	Name              string                `json:"name"`
	Label             string                `json:"label"`
	FieldType         string                `json:"fieldType"`
	EnumOptions       []string              `json:"enumOptions"`
	Editable          bool                  `json:"editable"`
	Required          bool                  `json:"required"`
	Visible           bool                  `json:"visible"`
	Searchable        bool                  `json:"searchable"`
	Sortable          bool                  `json:"sortable"`
	Position          int                   `json:"position"`
	Validations       []meta.ValidationRule `json:"validations"`
	BaseType          string                `json:"baseType"`
	FKTableDefID      string                `json:"fkTableDefId"`
	FKRefColumn       string                `json:"fkRefColumn"`
	FKDisplayColumns  []string              `json:"fkDisplayColumns"`
	M2MJunctionDefID  string                `json:"m2mJunctionDefId"`
	M2MJunctionSrcCol string                `json:"m2mJunctionSrcCol"`
	M2MJunctionTgtCol string                `json:"m2mJunctionTgtCol"`
	M2MDisplayColumns []string              `json:"m2mDisplayColumns"`
	IsComputed        bool                  `json:"isComputed"`
	ComputedFormula   string                `json:"computedFormula"`
	Formatting        json.RawMessage       `json:"formatting"`
}

func (s *Server) toCols(in []columnInput) []meta.ColumnDef {
	out := make([]meta.ColumnDef, 0, len(in))
	for _, c := range in {
		m := meta.ColumnDef{Name: c.Name, Label: c.Label, FieldType: c.FieldType,
			EnumOptions: c.EnumOptions, Editable: c.Editable, Required: c.Required,
			Visible: c.Visible, Searchable: c.Searchable, Sortable: c.Sortable,
			Position: c.Position, Validations: c.Validations, BaseType: c.BaseType,
			FKRefColumn: c.FKRefColumn, FKDisplayColumns: c.FKDisplayColumns,
			M2MJunctionSrcCol: c.M2MJunctionSrcCol, M2MJunctionTgtCol: c.M2MJunctionTgtCol,
			M2MDisplayColumns: c.M2MDisplayColumns,
			IsComputed:        c.IsComputed, ComputedFormula: c.ComputedFormula}
		if c.Formatting != nil {
			m.Formatting = string(c.Formatting)
		}
		if c.FKTableDefID == "self" {
			m.FKTableDefID = meta.SelfRef
		} else if c.FKTableDefID != "" {
			if id, err := s.ids.Decode("td", c.FKTableDefID); err == nil {
				m.FKTableDefID = id
			}
		}
		if c.M2MJunctionDefID != "" {
			if id, err := s.ids.Decode("td", c.M2MJunctionDefID); err == nil {
				m.M2MJunctionDefID = id
			}
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) toDef(in tableDefInput) (*meta.TableDef, error) {
	dsID, err := s.ids.Decode("ds", in.DatasourceID)
	if err != nil {
		return nil, errors.New("invalid datasourceId")
	}
	if in.DefaultSortDir != "DESC" {
		in.DefaultSortDir = "ASC"
	}
	if in.DefaultView != "kanban" && in.DefaultView != "calendar" {
		in.DefaultView = "grid"
	}
	in.Description = strings.TrimSpace(in.Description)
	if len(in.Description) > 200 {
		return nil, errors.New("description too long (max 200 chars)")
	}
	if in.SourceType != "query" {
		in.SourceType = "table"
		in.QuerySQL = ""
	}
	if in.SourceType == "query" {
		in.SchemaName, in.TableName = "", ""
	}
	return &meta.TableDef{DatasourceID: dsID, SchemaName: in.SchemaName,
		TableName: in.TableName, Label: in.Label, Description: in.Description,
		KeyColumns:     in.KeyColumns,
		PageSize:       in.PageSize,
		DefaultSortCol: in.DefaultSortCol, DefaultSortDir: in.DefaultSortDir,
		DefaultView: in.DefaultView, ViewConfig: string(in.ViewConfig), Hooks: string(in.Hooks),
		Actions:    string(in.Actions),
		SourceType: in.SourceType, QuerySQL: in.QuerySQL}, nil
}

type permsDTO struct {
	Read   bool `json:"read"`
	Create bool `json:"create"`
	Update bool `json:"update"`
	Delete bool `json:"delete"`
}

type columnDTO struct {
	Name              string                `json:"name"`
	Label             string                `json:"label"`
	FieldType         string                `json:"fieldType"`
	EnumOptions       []string              `json:"enumOptions"`
	Editable          bool                  `json:"editable"`
	Required          bool                  `json:"required"`
	Visible           bool                  `json:"visible"`
	Searchable        bool                  `json:"searchable"`
	Sortable          bool                  `json:"sortable"`
	Position          int                   `json:"position"`
	Validations       []meta.ValidationRule `json:"validations,omitempty"`
	BaseType          string                `json:"baseType,omitempty"`
	FKTableDefID      string                `json:"fkTableDefId,omitempty"`
	FKRefColumn       string                `json:"fkRefColumn,omitempty"`
	FKDisplayColumns  []string              `json:"fkDisplayColumns,omitempty"`
	M2MJunctionDefID  string                `json:"m2mJunctionDefId,omitempty"`
	M2MJunctionSrcCol string                `json:"m2mJunctionSrcCol,omitempty"`
	M2MJunctionTgtCol string                `json:"m2mJunctionTgtCol,omitempty"`
	M2MDisplayColumns []string              `json:"m2mDisplayColumns,omitempty"`
	IsComputed        bool                  `json:"isComputed,omitempty"`
	ComputedFormula   string                `json:"computedFormula,omitempty"`
	Formatting        json.RawMessage       `json:"formatting,omitempty"`
	// M2MRefColumn is the source-table column the junction references —
	// resolved server-side so the grid can key m2mRels lookups.
	M2MRefColumn string `json:"m2mRefColumn,omitempty"`
	// M2MTargetRef is the target-table column used as the link value.
	M2MTargetRef string `json:"m2mTargetRef,omitempty"`
}

func (s *Server) colToDTO(c meta.ColumnDef, m2mRefCache *map[string][2]string) columnDTO {
	dto := columnDTO{Name: c.Name, Label: c.Label, FieldType: c.FieldType,
		EnumOptions: c.EnumOptions, Editable: c.Editable, Required: c.Required,
		Visible: c.Visible, Searchable: c.Searchable, Sortable: c.Sortable,
		Position: c.Position, Validations: c.Validations, BaseType: c.BaseType,
		FKRefColumn: c.FKRefColumn, FKDisplayColumns: c.FKDisplayColumns,
		M2MJunctionSrcCol: c.M2MJunctionSrcCol, M2MJunctionTgtCol: c.M2MJunctionTgtCol,
		M2MDisplayColumns: c.M2MDisplayColumns,
		IsComputed:        c.IsComputed, ComputedFormula: c.ComputedFormula}
	if c.Formatting != "" {
		dto.Formatting = json.RawMessage(c.Formatting)
	}
	if c.FKTableDefID > 0 {
		dto.FKTableDefID = s.ids.Encode("td", c.FKTableDefID)
	}
	if c.M2MJunctionDefID > 0 {
		dto.M2MJunctionDefID = s.ids.Encode("td", c.M2MJunctionDefID)
		cacheKey := fmt.Sprintf("%d|%s|%s", c.M2MJunctionDefID, c.M2MJunctionSrcCol, c.M2MJunctionTgtCol)
		if v, ok := (*m2mRefCache)[cacheKey]; ok {
			dto.M2MRefColumn, dto.M2MTargetRef = v[0], v[1]
		} else if _, jcols, err := s.store.GetTableDef(c.M2MJunctionDefID); err == nil {
			for _, jc := range jcols {
				if jc.Name == c.M2MJunctionSrcCol && jc.FieldType == "fk" {
					dto.M2MRefColumn = jc.FKRefColumn
				}
				if jc.Name == c.M2MJunctionTgtCol && jc.FieldType == "fk" {
					dto.M2MTargetRef = jc.FKRefColumn
				}
			}
			(*m2mRefCache)[cacheKey] = [2]string{dto.M2MRefColumn, dto.M2MTargetRef}
		}
	}
	return dto
}

// tableDefDTO masks ids and carries the caller's grants.
type tableDefDTO struct {
	ID             string          `json:"id"`
	DatasourceID   string          `json:"datasourceId"`
	SchemaName     string          `json:"schemaName"`
	TableName      string          `json:"tableName"`
	Label          string          `json:"label"`
	Description    string          `json:"description"`
	KeyColumns     []string        `json:"keyColumns"`
	PageSize       int             `json:"pageSize"`
	DefaultSortCol string          `json:"defaultSortCol"`
	DefaultSortDir string          `json:"defaultSortDir"`
	DefaultView    string          `json:"defaultView,omitempty"`
	ViewConfig     json.RawMessage `json:"viewConfig,omitempty"`
	Hooks          json.RawMessage `json:"hooks,omitempty"`
	Actions        json.RawMessage `json:"actions,omitempty"`
	SourceType     string          `json:"sourceType,omitempty"`
	QuerySQL       string          `json:"querySql,omitempty"`
	GroupID        string          `json:"groupId,omitempty"`
	GroupName      string          `json:"groupName,omitempty"`
	Columns        []columnDTO     `json:"columns,omitempty"`
	Permissions    permsDTO        `json:"permissions"`
}

func (s *Server) toTableDTO(def *meta.TableDef, cols []meta.ColumnDef, p permsDTO, groups map[int64]string) tableDefDTO {
	dto := tableDefDTO{
		ID:             s.ids.Encode("td", def.ID),
		DatasourceID:   s.ids.Encode("ds", def.DatasourceID),
		SchemaName:     def.SchemaName,
		TableName:      def.TableName,
		Label:          def.Label,
		Description:    def.Description,
		KeyColumns:     def.KeyColumns,
		PageSize:       def.PageSize,
		DefaultSortCol: def.DefaultSortCol,
		DefaultSortDir: def.DefaultSortDir,
		DefaultView:    def.DefaultView,
		SourceType:     def.SourceType,
		QuerySQL:       def.QuerySQL,
		Permissions:    p,
	}
	if def.GroupID > 0 {
		dto.GroupID = s.ids.Encode("grp", def.GroupID)
		dto.GroupName = groups[def.GroupID]
	}
	if def.ViewConfig != "" {
		dto.ViewConfig = json.RawMessage(def.ViewConfig)
	}
	if def.Hooks != "" {
		dto.Hooks = json.RawMessage(def.Hooks)
	}
	if def.Actions != "" {
		dto.Actions = json.RawMessage(def.Actions)
	}
	if dto.KeyColumns == nil {
		dto.KeyColumns = []string{}
	}
	m2mRefCache := map[string][2]string{}
	for _, c := range cols {
		dto.Columns = append(dto.Columns, s.colToDTO(c, &m2mRefCache))
	}
	return dto
}

// tablePerms resolves the caller's grants for a table def. Only Admin has
// implicit full access; everyone else (including platform managers) needs
// stored per-table grants — platform management and table CRUD are separate
// permission dimensions. Query views are always read-only: create/update/
// delete are zeroed regardless of grants (this is what makes the frontend
// grid render read-only with no client changes).
func (s *Server) tablePerms(u CtxUser, def *meta.TableDef) permsDTO {
	var p permsDTO
	if u.IsAdmin {
		p = permsDTO{true, true, true, true}
	} else {
		g, err := s.store.GrantsFor(u.RoleID, def.ID)
		if err != nil {
			return permsDTO{}
		}
		p = permsDTO{g.CanRead, g.CanCreate, g.CanUpdate, g.CanDelete}
	}
	if isQueryDef(def) {
		p.Create, p.Update, p.Delete = false, false, false
	}
	return p
}

// hasTablePerm checks one row action ("read"|"create"|"update"|"delete").
func (s *Server) hasTablePerm(u CtxUser, defID int64, action string) bool {
	if u.IsAdmin {
		return true
	}
	g, err := s.store.GrantsFor(u.RoleID, defID)
	if err != nil {
		return false
	}
	switch action {
	case "read":
		return g.CanRead
	case "create":
		return g.CanCreate
	case "update":
		return g.CanUpdate
	case "delete":
		return g.CanDelete
	}
	return false
}

var validFieldTypes = map[string]bool{
	"boolean": true, "text": true, "number": true, "datetime": true, "enum": true,
	"uuid": true, "json": true, "fk": true, "m2m": true,
}

var validRules = map[string]bool{"email": true, "min_len": true, "max_len": true, "number": true, "text": true}

var validEnumColors = map[string]bool{
	"gray": true, "blue": true, "green": true, "amber": true,
	"red": true, "purple": true, "cyan": true, "orange": true,
}

// checkFormatting validates a column's raw formatting JSON.
func checkFormatting(c meta.ColumnDef) string {
	if c.Formatting == "" {
		return ""
	}
	var f struct {
		EnumColors map[string]string `json:"enumColors"`
		Number     *struct {
			Thousands *bool  `json:"thousands"`
			Decimals  *int   `json:"decimals"`
			Prefix    string `json:"prefix"`
		} `json:"number"`
	}
	if err := json.Unmarshal([]byte(c.Formatting), &f); err != nil {
		return "column " + c.Name + ": formatting is not valid JSON"
	}
	if len(f.EnumColors) > 0 && c.FieldType != "enum" {
		return "column " + c.Name + ": enumColors requires an enum column"
	}
	for v, col := range f.EnumColors {
		if !validEnumColors[col] {
			return "column " + c.Name + ": unknown enum color " + col + " for value " + v
		}
	}
	if f.Number != nil && c.FieldType != "number" {
		return "column " + c.Name + ": number formatting requires a number column"
	}
	if f.Number != nil && f.Number.Decimals != nil && (*f.Number.Decimals < 0 || *f.Number.Decimals > 6) {
		return "column " + c.Name + ": decimals must be 0..6"
	}
	return ""
}

var (
	errDSNotFound = errors.New("datasource not found")
	errConn       = errors.New("connection failed")
)

type viewConfigJSON struct {
	KanbanBoardColumn   string `json:"kanbanBoardColumn"`
	KanbanDisplayColumn string `json:"kanbanDisplayColumn"`
	CalendarStartColumn string `json:"calendarStartColumn"`
	CalendarEndColumn   string `json:"calendarEndColumn"`
}

func (s *Server) checkViewConfig(def *meta.TableDef, cols []meta.ColumnDef) string {
	if def.DefaultView != "grid" && def.DefaultView != "kanban" && def.DefaultView != "calendar" {
		return "defaultView must be grid, kanban or calendar"
	}
	if def.ViewConfig == "" {
		return ""
	}
	var vc viewConfigJSON
	if err := json.Unmarshal([]byte(def.ViewConfig), &vc); err != nil {
		return "viewConfig is not valid JSON"
	}
	byName := map[string]meta.ColumnDef{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	board, boardOk := byName[vc.KanbanBoardColumn]
	if vc.KanbanBoardColumn != "" {
		if !boardOk || board.FieldType != "enum" || board.IsComputed {
			return "viewConfig.kanbanBoardColumn must be a defined, non-computed enum column"
		}
	}
	if vc.KanbanDisplayColumn != "" {
		disp, ok := byName[vc.KanbanDisplayColumn]
		if !ok || !disp.Visible {
			return "viewConfig.kanbanDisplayColumn must be a defined, visible column"
		}
	}
	if vc.CalendarStartColumn != "" {
		start, ok := byName[vc.CalendarStartColumn]
		if !ok || start.FieldType != "datetime" || !start.Visible {
			return "viewConfig.calendarStartColumn must be a defined, visible datetime column"
		}
	}
	if vc.CalendarEndColumn != "" {
		end, ok := byName[vc.CalendarEndColumn]
		if !ok || end.FieldType != "datetime" || !end.Visible {
			return "viewConfig.calendarEndColumn must be a defined, visible datetime column"
		}
	}
	if vc.CalendarEndColumn != "" && vc.CalendarStartColumn == "" {
		return "viewConfig.calendarEndColumn requires calendarStartColumn"
	}
	return ""
}

const querySQLMax = 20000

// checkQuerySQL enforces the query-view SQL constraints: non-empty, within
// the size cap, and SELECT/WITH-prefixed. This is a lexical guard only —
// the real validation is the EXPLAIN run at save time.
func checkQuerySQL(q string) string {
	if q == "" {
		return "querySql is required for query views"
	}
	if len(q) > querySQLMax {
		return "querySql exceeds 20000 characters"
	}
	head := strings.ToUpper(strings.TrimSpace(q))
	if !strings.HasPrefix(head, "SELECT") && !strings.HasPrefix(head, "WITH") {
		return "query must start with SELECT or WITH"
	}
	return ""
}

func isQueryDef(def *meta.TableDef) bool { return def.SourceType == "query" }

// writeQueryErr maps query-view execution failures: statement timeouts get
// their own 502 code so clients can tell slow queries from broken ones.
func writeQueryErr(w http.ResponseWriter, err error) {
	if ds.IsQueryTimeout(err) {
		writeErr(w, 502, "QUERY_TIMEOUT", "query exceeded the execution time limit", err.Error())
		return
	}
	writeErr(w, 502, "CONN", "query failed", err.Error())
}

// writeQueryReadOnly rejects any write attempt against a query view.
func writeQueryReadOnly(w http.ResponseWriter, def *meta.TableDef) bool {
	if isQueryDef(def) {
		writeErr(w, 403, "QUERY_READONLY", "query views are read-only", nil)
		return true
	}
	return false
}

// explainQueryDef runs EXPLAIN-on-save for query defs. Returns true when it
// already wrote the error response. A datasource that cannot be reached
// fails validation too — a query view may only be saved in a state the
// database has vouched for.
func (s *Server) explainQueryDef(w http.ResponseWriter, def *meta.TableDef) bool {
	if !isQueryDef(def) {
		return false
	}
	a, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		if errors.Is(err, errDSNotFound) {
			s.writeLiveErr(w, err)
			return true
		}
		writeErr(w, 400, "QUERY_INVALID", "query failed validation", err.Error())
		return true
	}
	defer a.Close()
	if err := a.ExplainQuery(def.QuerySQL); err != nil {
		writeErr(w, 400, "QUERY_INVALID", "query failed validation", err.Error())
		return true
	}
	return false
}

func (s *Server) validateDef(def *meta.TableDef, cols []meta.ColumnDef) string {
	query := isQueryDef(def)
	if def.DatasourceID == 0 || def.Label == "" {
		return "datasourceId and label are required"
	}
	if query {
		if msg := checkQuerySQL(def.QuerySQL); msg != "" {
			return msg
		}
	} else if def.SchemaName == "" || def.TableName == "" || len(def.KeyColumns) == 0 {
		return "datasourceId, schemaName, tableName, label, keyColumns are required"
	}
	if def.PageSize <= 0 || def.PageSize > 200 {
		return "pageSize must be 1..200"
	}
	if !query {
		for _, name := range append([]string{def.SchemaName, def.TableName}, def.KeyColumns...) {
			if _, err := ds.QuoteIdent(name); err != nil {
				return "invalid identifier: " + name
			}
		}
	}
	keySeen := make([]bool, len(def.KeyColumns))
	sortable := map[string]bool{}
	ccols := toCore(def, cols).Columns
	for i, c := range cols {
		sortable[c.Name] = c.Sortable
		if !validFieldTypes[c.FieldType] {
			return "column " + c.Name + ": invalid fieldType " + c.FieldType
		}
		if query && (c.FieldType == "fk" || c.FieldType == "m2m") {
			return "column " + c.Name + ": query views cannot use fk or m2m columns"
		}
		if query && len(c.Validations) > 0 {
			return "column " + c.Name + ": query views cannot define validation rules"
		}
		if c.FieldType == "enum" && len(c.EnumOptions) == 0 {
			return "column " + c.Name + ": enum needs options"
		}
		ruleSeen := map[string]bool{}
		for _, r := range c.Validations {
			if !validRules[r.Type] {
				return "column " + c.Name + ": invalid validation rule " + r.Type
			}
			if ruleSeen[r.Type] {
				return "column " + c.Name + ": duplicate validation rule " + r.Type
			}
			ruleSeen[r.Type] = true
			if (r.Type == "min_len" || r.Type == "max_len") && (r.Param < 1 || r.Param > 1000) {
				return "column " + c.Name + ": validation rule param must be 1..1000"
			}
		}
		if msg := checkFormatting(c); msg != "" {
			return msg
		}
		if c.Name == "" || c.Label == "" {
			return "column name and label are required"
		}
		if _, err := ds.QuoteIdent(c.Name); err != nil {
			return "invalid identifier: " + c.Name
		}
		if c.IsComputed {
			if c.FieldType != "number" && c.FieldType != "text" {
				return "column " + c.Name + ": computed columns must be number or text"
			}
			if c.Editable || c.Searchable || c.Sortable {
				return "column " + c.Name + ": computed columns cannot be editable/searchable/sortable"
			}
			for _, key := range def.KeyColumns {
				if c.Name == key {
					return "column " + c.Name + ": computed columns cannot be key columns"
				}
			}
			if c.ComputedFormula == "" {
				return "column " + c.Name + ": computed columns need computedFormula"
			}
			ft, _, err := engine.CompileComputed(ccols[i], ccols)
			if err != nil {
				return "column " + c.Name + ": " + err.Error()
			}
			if ft != c.FieldType {
				return "column " + c.Name + ": formula produces " + ft + " but the column type is " + c.FieldType
			}
			continue
		}
		if msg := s.validateFK(def, cols, c); msg != "" {
			return msg
		}
		if msg := s.validateM2M(def, cols, c); msg != "" {
			return msg
		}
		for k, key := range def.KeyColumns {
			if c.Name == key {
				if c.FieldType == "m2m" {
					return "key column " + c.Name + " cannot be a many-to-many relation"
				}
				keySeen[k] = true
			}
		}
		for j := 0; j < i; j++ {
			if cols[j].Name == c.Name || cols[j].Position == c.Position {
				return "column names and positions must be unique"
			}
		}
	}
	for k, seen := range keySeen {
		if !seen {
			return "key column " + def.KeyColumns[k] + " must be one of the defined columns"
		}
	}
	if def.DefaultSortCol != "" && !sortable[def.DefaultSortCol] {
		return "defaultSortCol must be a defined, sortable column"
	}
	if msg := s.checkViewConfig(def, cols); msg != "" {
		return msg
	}
	if query && def.Hooks != "" {
		return "query views cannot assign hooks"
	}
	if !query {
		if msg := s.checkHooks(def); msg != "" {
			return msg
		}
	}
	if msg := s.checkActions(def); msg != "" {
		return msg
	}
	return ""
}

// checkHooks rejects assignments that don't parse or that name hooks absent
// from this binary's registry.
func (s *Server) checkHooks(def *meta.TableDef) string {
	if def.Hooks == "" {
		return ""
	}
	asgs, err := hooks.ParseAssignments(def.Hooks)
	if err != nil {
		return err.Error()
	}
	if err := s.hooks.CheckMissing(asgs); err != nil {
		return err.Error()
	}
	return ""
}

// checkActions rejects action configs that don't parse, name hooks absent
// from this binary, or attach custom actions to query views (hidden keys
// alone are fine — query grids still honor export/refresh visibility).
func (s *Server) checkActions(def *meta.TableDef) string {
	cfg, err := hooks.ParseActions(def.Actions)
	if err != nil {
		return err.Error()
	}
	if isQueryDef(def) && len(cfg.Custom) > 0 {
		return "query views cannot define custom actions"
	}
	for _, a := range cfg.Custom {
		if _, ok := s.hooks.Get(a.Hook); !ok {
			return "action " + a.ID + ": hook " + a.Hook + " is not registered in this binary"
		}
	}
	return ""
}

// validateM2M checks one column's m2m payload (mirror of validateFK).
// Junction/target resolution runs in the engine (engine.ResolveM2M) over
// the name-based core contract, so the row endpoints and the def-save
// validation share one set of messages.
func (s *Server) validateM2M(def *meta.TableDef, cols []meta.ColumnDef, c meta.ColumnDef) string {
	if c.FieldType != "m2m" {
		if c.M2MJunctionDefID != 0 || c.M2MJunctionSrcCol != "" || c.M2MJunctionTgtCol != "" || len(c.M2MDisplayColumns) > 0 {
			return "column " + c.Name + ": m2m fields require fieldType \"m2m\""
		}
		return ""
	}
	if c.M2MJunctionDefID == 0 {
		return "column " + c.Name + ": m2m needs m2mJunctionDefId"
	}
	if len(c.M2MDisplayColumns) == 0 {
		return "column " + c.Name + ": m2m needs at least one display column"
	}
	if def.ID == 0 {
		return "column " + c.Name + ": save this table definition before adding many-to-many relations"
	}
	res := s.metaRes(def)
	ct := meta.ToCoreDef(*def, cols, res.idToName)
	var coreCol defs.Column
	for _, cc := range ct.Columns {
		if cc.Name == c.Name {
			coreCol = cc
			break
		}
	}
	cfg, msg := engine.ResolveM2M(res, &ct, coreCol)
	if cfg == nil {
		return msg
	}
	if cfg.TargetMissing {
		return "column " + c.Name + ": m2m target definition not found"
	}
	names := map[string]bool{}
	for _, tc := range cfg.Target.Columns {
		names[tc.Name] = true
	}
	seen := map[string]bool{}
	for _, d := range c.M2MDisplayColumns {
		if !names[d] || seen[d] {
			return "column " + c.Name + ": m2mDisplayColumns must match target columns"
		}
		seen[d] = true
	}
	return ""
}

// validateFK checks one column's fk payload. targetCols resolves the target
// definition's columns (the def itself for self-references).
func (s *Server) validateFK(def *meta.TableDef, cols []meta.ColumnDef, c meta.ColumnDef) string {
	if c.FieldType != "fk" {
		if c.BaseType != "" || c.FKTableDefID != 0 || c.FKRefColumn != "" || len(c.FKDisplayColumns) > 0 {
			return "column " + c.Name + ": fk fields require fieldType \"fk\""
		}
		return ""
	}
	if !validFieldTypes[c.BaseType] || c.BaseType == "fk" {
		return "column " + c.Name + ": fk needs a valid baseType"
	}
	if len(c.FKDisplayColumns) == 0 {
		return "column " + c.Name + ": fk needs at least one display column"
	}
	if c.FKRefColumn == "" {
		return "column " + c.Name + ": fk needs fkRefColumn"
	}
	var targetCols []meta.ColumnDef
	switch {
	case c.FKTableDefID == meta.SelfRef, def.ID != 0 && c.FKTableDefID == def.ID:
		targetCols = cols // this definition's own incoming columns
	case c.FKTableDefID == 0:
		return "column " + c.Name + ": fk needs fkTableDefId"
	default:
		_, tc, err := s.store.GetTableDef(c.FKTableDefID)
		if err != nil {
			return "column " + c.Name + ": fk target definition not found"
		}
		targetCols = tc
	}
	names := map[string]bool{}
	for _, t := range targetCols {
		names[t.Name] = true
	}
	if !names[c.FKRefColumn] {
		return "column " + c.Name + ": fkRefColumn not on target table"
	}
	seen := map[string]bool{}
	for _, d := range c.FKDisplayColumns {
		if !names[d] || seen[d] {
			return "column " + c.Name + ": fkDisplayColumns must match target columns"
		}
		seen[d] = true
	}
	return ""
}

func (s *Server) handleTableCreate(w http.ResponseWriter, r *http.Request) {
	var in tableDefInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	def, err := s.toDef(in)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	cols := s.toCols(in.Columns)
	if msg := s.validateDef(def, cols); msg != "" {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	if s.explainQueryDef(w, def) {
		return
	}
	if err := s.store.SaveTableDef(def, cols); err != nil {
		writeErr(w, 400, "VALIDATION", "save failed", err.Error())
		return
	}
	writeJSON(w, 200, s.toTableDTO(def, cols, s.tablePerms(userFrom(r), def), s.groupNameMap()))
}

func (s *Server) handleTableList(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	list, err := s.store.ListTableDefs()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	out := []tableDefDTO{}
	groups := s.groupNameMap()
	for i := range list {
		p := s.tablePerms(u, &list[i])
		// Table managers see every definition (they define them); everyone
		// else only sees tables they can read. The permissions object always
		// reflects actual row-CRUD grants.
		if !u.ManageTables && !p.Read {
			continue
		}
		out = append(out, s.toTableDTO(&list[i], nil, p, groups))
	}
	writeJSON(w, 200, out)
}

func (s *Server) tableCtx(r *http.Request) (*meta.TableDef, []meta.ColumnDef, error) {
	id, err := s.ids.Decode("td", r.PathValue("id"))
	if err != nil {
		return nil, nil, meta.ErrNotFound
	}
	return s.store.GetTableDef(id)
}

func (s *Server) writeDefErr(w http.ResponseWriter, err error) {
	if errors.Is(err, meta.ErrNotFound) {
		writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
		return
	}
	writeErr(w, 500, "INTERNAL", "server error", nil)
}

func (s *Server) handleTableGet(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	p := s.tablePerms(u, def)
	if !u.ManageTables && !p.Read {
		writeErr(w, 403, "FORBIDDEN", "no access to this table", nil)
		return
	}
	writeJSON(w, 200, s.toTableDTO(def, cols, p, s.groupNameMap()))
}

func (s *Server) handleTableUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := s.ids.Decode("td", r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
		return
	}
	var in tableDefInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	def, err := s.toDef(in)
	if err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	cols := s.toCols(in.Columns)
	def.ID = id
	// group assignment is managed via PATCH /api/tables/{id}; keep it on edit
	if old, _, err := s.store.GetTableDef(id); err == nil {
		def.GroupID = old.GroupID
	}
	if msg := s.validateDef(def, cols); msg != "" {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	if s.explainQueryDef(w, def) {
		return
	}
	if err := s.store.UpdateTableDef(def, cols); err != nil {
		if errors.Is(err, meta.ErrNotFound) {
			writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
			return
		}
		writeErr(w, 400, "VALIDATION", "update failed", err.Error())
		return
	}
	writeJSON(w, 200, s.toTableDTO(def, cols, s.tablePerms(userFrom(r), def), s.groupNameMap()))
}

func (s *Server) handleTableDelete(w http.ResponseWriter, r *http.Request) {
	id, err := s.ids.Decode("td", r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
		return
	}
	err = s.store.DeleteTableDef(id)
	switch {
	case errors.Is(err, meta.ErrNotFound):
		writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
	case err != nil:
		writeErr(w, 500, "INTERNAL", "server error", nil)
	default:
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

// liveAdapter opens the live connection for a datasource id.
func (s *Server) liveAdapter(dsID int64) (ds.Adapter, error) {
	d, err := s.store.GetDatasource(dsID)
	if errors.Is(err, meta.ErrNotFound) {
		return nil, errDSNotFound
	}
	if err != nil {
		return nil, err
	}
	a, err := ds.Open(ds.Conn{Driver: d.Driver, Host: d.Host, Port: d.Port,
		DB: d.DBName, User: d.Username, Password: d.Password, SSLMode: d.SSLMode, Raw: d.Raw})
	if err != nil {
		return nil, errConn
	}
	return a, nil
}

func (s *Server) writeLiveErr(w http.ResponseWriter, err error) {
	if errors.Is(err, errDSNotFound) {
		writeErr(w, 404, "NOT_FOUND", "datasource not found", nil)
		return
	}
	if errors.Is(err, errConn) {
		writeErr(w, 502, "CONN", "could not connect to datasource", err.Error())
		return
	}
	writeErr(w, 500, "INTERNAL", "server error", nil)
}

func (s *Server) handleDSTables(w http.ResponseWriter, r *http.Request) {
	id, err := s.dsCtx(r)
	if err != nil {
		s.writeDSErr(w, err)
		return
	}
	d, err := s.store.GetDatasource(id)
	if err != nil {
		s.writeDSErr(w, err)
		return
	}
	a, err := ds.Open(ds.Conn{Driver: d.Driver, Host: d.Host, Port: d.Port,
		DB: d.DBName, User: d.Username, Password: d.Password, SSLMode: d.SSLMode, Raw: d.Raw})
	if err != nil {
		s.writeLiveErr(w, errConn)
		return
	}
	defer a.Close()
	tables, err := a.ListTables()
	if err != nil {
		writeErr(w, 502, "CONN", "introspection failed", err.Error())
		return
	}
	writeJSON(w, 200, tables)
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	db, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()
	var live []ds.LiveColumn
	if isQueryDef(def) {
		if err := db.ExplainQuery(def.QuerySQL); err != nil {
			writeErr(w, 502, "CONN", "query validation failed", err.Error())
			return
		}
		live, _, err = db.IntrospectQuery(def.QuerySQL)
	} else {
		live, err = db.InspectTable(def.SchemaName, def.TableName)
	}
	if err != nil {
		writeErr(w, 502, "CONN", "introspection failed", err.Error())
		return
	}
	rep := ds.CompareDrift(meta.ToCoreDef(*def, cols, nil).Columns, live)
	if !rep.Empty() {
		writeErr(w, 409, "DRIFT", "table definition is out of sync with the live schema", rep)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleResync(w http.ResponseWriter, r *http.Request) {
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	db, err := s.liveAdapter(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()
	var live []ds.LiveColumn
	if isQueryDef(def) {
		live, _, err = db.IntrospectQuery(def.QuerySQL)
	} else {
		live, err = db.InspectTable(def.SchemaName, def.TableName)
	}
	if err != nil {
		writeErr(w, 502, "CONN", "introspection failed", err.Error())
		return
	}
	missing := ds.CompareDrift(meta.ToCoreDef(*def, cols, nil).Columns, live).Missing
	for _, m := range missing {
		for _, key := range def.KeyColumns {
			if m == key {
				writeErr(w, 409, "DRIFT", "key column "+m+" was dropped; edit the definition manually", nil)
				return
			}
		}
	}

	liveByName := map[string]ds.LiveColumn{}
	for _, c := range live {
		liveByName[c.Name] = c
	}
	maxPos := -1
	for _, c := range cols {
		if c.Position > maxPos {
			maxPos = c.Position
		}
	}

	var out []meta.ColumnDef
	for _, c := range cols {
		if c.FieldType == "m2m" || c.IsComputed {
			out = append(out, c) // virtual column — preserved on resync
			continue
		}
		lc, ok := liveByName[c.Name]
		if !ok {
			continue // dropped
		}
		if ds.EffectiveType(defs.Column{FieldType: c.FieldType, BaseType: c.BaseType}) != lc.FieldType {
			if c.FieldType == "fk" {
				c.BaseType = lc.FieldType // keep relation config; only base drifts
			} else {
				c.FieldType = lc.FieldType
				if lc.FieldType == "enum" {
					c.EnumOptions = lc.EnumOptions
				} else {
					c.EnumOptions = nil
				}
			}
		}
		out = append(out, c)
	}
	defNames := map[string]bool{}
	for _, c := range cols {
		defNames[c.Name] = true
	}
	for _, lc := range live {
		if defNames[lc.Name] {
			continue
		}
		maxPos++
		out = append(out, meta.ColumnDef{
			Name: lc.Name, Label: lc.Name, FieldType: lc.FieldType,
			EnumOptions: lc.EnumOptions,
			Editable:    true, Visible: true, Searchable: true, Sortable: true,
			Position: maxPos,
		})
	}
	if err := s.store.ReplaceColumns(def.ID, out); err != nil {
		writeErr(w, 500, "INTERNAL", "resync failed", err.Error())
		return
	}
	_, fresh, _ := s.store.GetTableDef(def.ID)
	writeJSON(w, 200, s.toTableDTO(def, fresh, permsDTO{true, true, true, true}, s.groupNameMap()))
}

func (s *Server) handleDSColumns(w http.ResponseWriter, r *http.Request) {
	id, err := s.dsCtx(r)
	if err != nil {
		s.writeDSErr(w, err)
		return
	}
	d, err := s.store.GetDatasource(id)
	if err != nil {
		s.writeDSErr(w, err)
		return
	}
	a, err := ds.Open(ds.Conn{Driver: d.Driver, Host: d.Host, Port: d.Port,
		DB: d.DBName, User: d.Username, Password: d.Password, SSLMode: d.SSLMode, Raw: d.Raw})
	if err != nil {
		s.writeLiveErr(w, errConn)
		return
	}
	defer a.Close()
	cols, err := a.InspectTable(r.PathValue("schema"), r.PathValue("table"))
	if err != nil {
		writeErr(w, 502, "CONN", "introspection failed", err.Error())
		return
	}
	if cols == nil {
		cols = []ds.LiveColumn{}
	}
	writeJSON(w, 200, cols)
}
