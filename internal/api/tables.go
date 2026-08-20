package api

import (
	"errors"
	"fmt"
	"net/http"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

// tableDefInput accepts masked datasource ids from the client.
type tableDefInput struct {
	DatasourceID   string        `json:"datasourceId"`
	SchemaName     string        `json:"schemaName"`
	TableName      string        `json:"tableName"`
	Label          string        `json:"label"`
	KeyColumns     []string      `json:"keyColumns"`
	PageSize       int           `json:"pageSize"`
	DefaultSortCol string        `json:"defaultSortCol"`
	DefaultSortDir string        `json:"defaultSortDir"`
	Columns        []columnInput `json:"columns"`
}

// columnInput mirrors meta.ColumnDef but takes the fk/m2m targets as masked
// tokens (fk target may also be the literal "self").
type columnInput struct {
	Name              string   `json:"name"`
	Label             string   `json:"label"`
	FieldType         string   `json:"fieldType"`
	EnumOptions       []string `json:"enumOptions"`
	Editable          bool     `json:"editable"`
	Required          bool     `json:"required"`
	Visible           bool     `json:"visible"`
	Searchable        bool     `json:"searchable"`
	Sortable          bool     `json:"sortable"`
	Position          int      `json:"position"`
	BaseType          string   `json:"baseType"`
	FKTableDefID      string   `json:"fkTableDefId"`
	FKRefColumn       string   `json:"fkRefColumn"`
	FKDisplayColumns  []string `json:"fkDisplayColumns"`
	M2MJunctionDefID  string   `json:"m2mJunctionDefId"`
	M2MJunctionSrcCol string   `json:"m2mJunctionSrcCol"`
	M2MJunctionTgtCol string   `json:"m2mJunctionTgtCol"`
	M2MDisplayColumns []string `json:"m2mDisplayColumns"`
}

func (s *Server) toCols(in []columnInput) []meta.ColumnDef {
	out := make([]meta.ColumnDef, 0, len(in))
	for _, c := range in {
		m := meta.ColumnDef{Name: c.Name, Label: c.Label, FieldType: c.FieldType,
			EnumOptions: c.EnumOptions, Editable: c.Editable, Required: c.Required,
			Visible: c.Visible, Searchable: c.Searchable, Sortable: c.Sortable,
			Position: c.Position, BaseType: c.BaseType,
			FKRefColumn: c.FKRefColumn, FKDisplayColumns: c.FKDisplayColumns,
			M2MJunctionSrcCol: c.M2MJunctionSrcCol, M2MJunctionTgtCol: c.M2MJunctionTgtCol,
			M2MDisplayColumns: c.M2MDisplayColumns}
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
	return &meta.TableDef{DatasourceID: dsID, SchemaName: in.SchemaName,
		TableName: in.TableName, Label: in.Label, KeyColumns: in.KeyColumns,
		PageSize:       in.PageSize,
		DefaultSortCol: in.DefaultSortCol, DefaultSortDir: in.DefaultSortDir}, nil
}

type permsDTO struct {
	Read   bool `json:"read"`
	Create bool `json:"create"`
	Update bool `json:"update"`
	Delete bool `json:"delete"`
}

type columnDTO struct {
	Name              string   `json:"name"`
	Label             string   `json:"label"`
	FieldType         string   `json:"fieldType"`
	EnumOptions       []string `json:"enumOptions"`
	Editable          bool     `json:"editable"`
	Required          bool     `json:"required"`
	Visible           bool     `json:"visible"`
	Searchable        bool     `json:"searchable"`
	Sortable          bool     `json:"sortable"`
	Position          int      `json:"position"`
	BaseType          string   `json:"baseType,omitempty"`
	FKTableDefID      string   `json:"fkTableDefId,omitempty"`
	FKRefColumn       string   `json:"fkRefColumn,omitempty"`
	FKDisplayColumns  []string `json:"fkDisplayColumns,omitempty"`
	M2MJunctionDefID  string   `json:"m2mJunctionDefId,omitempty"`
	M2MJunctionSrcCol string   `json:"m2mJunctionSrcCol,omitempty"`
	M2MJunctionTgtCol string   `json:"m2mJunctionTgtCol,omitempty"`
	M2MDisplayColumns []string `json:"m2mDisplayColumns,omitempty"`
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
		Position: c.Position, BaseType: c.BaseType,
		FKRefColumn: c.FKRefColumn, FKDisplayColumns: c.FKDisplayColumns,
		M2MJunctionSrcCol: c.M2MJunctionSrcCol, M2MJunctionTgtCol: c.M2MJunctionTgtCol,
		M2MDisplayColumns: c.M2MDisplayColumns}
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
	ID             string      `json:"id"`
	DatasourceID   string      `json:"datasourceId"`
	SchemaName     string      `json:"schemaName"`
	TableName      string      `json:"tableName"`
	Label          string      `json:"label"`
	KeyColumns     []string    `json:"keyColumns"`
	PageSize       int         `json:"pageSize"`
	DefaultSortCol string      `json:"defaultSortCol"`
	DefaultSortDir string      `json:"defaultSortDir"`
	Columns        []columnDTO `json:"columns,omitempty"`
	Permissions    permsDTO    `json:"permissions"`
}

func (s *Server) toTableDTO(def *meta.TableDef, cols []meta.ColumnDef, p permsDTO) tableDefDTO {
	dto := tableDefDTO{
		ID:             s.ids.Encode("td", def.ID),
		DatasourceID:   s.ids.Encode("ds", def.DatasourceID),
		SchemaName:     def.SchemaName,
		TableName:      def.TableName,
		Label:          def.Label,
		KeyColumns:     def.KeyColumns,
		PageSize:       def.PageSize,
		DefaultSortCol: def.DefaultSortCol,
		DefaultSortDir: def.DefaultSortDir,
		Permissions:    p,
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
// permission dimensions.
func (s *Server) tablePerms(u CtxUser, defID int64) permsDTO {
	if u.IsAdmin {
		return permsDTO{true, true, true, true}
	}
	g, err := s.store.GrantsFor(u.RoleID, defID)
	if err != nil {
		return permsDTO{}
	}
	return permsDTO{g.CanRead, g.CanCreate, g.CanUpdate, g.CanDelete}
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

var (
	errDSNotFound = errors.New("datasource not found")
	errConn       = errors.New("connection failed")
)

func (s *Server) validateDef(def *meta.TableDef, cols []meta.ColumnDef) string {
	if def.DatasourceID == 0 || def.SchemaName == "" || def.TableName == "" ||
		def.Label == "" || len(def.KeyColumns) == 0 {
		return "datasourceId, schemaName, tableName, label, keyColumns are required"
	}
	if def.PageSize <= 0 || def.PageSize > 200 {
		return "pageSize must be 1..200"
	}
	for _, name := range append([]string{def.SchemaName, def.TableName}, def.KeyColumns...) {
		if _, err := ds.QuoteIdent(name); err != nil {
			return "invalid identifier: " + name
		}
	}
	keySeen := make([]bool, len(def.KeyColumns))
	sortable := map[string]bool{}
	for i, c := range cols {
		sortable[c.Name] = c.Sortable
		if !validFieldTypes[c.FieldType] {
			return "column " + c.Name + ": invalid fieldType " + c.FieldType
		}
		if c.FieldType == "enum" && len(c.EnumOptions) == 0 {
			return "column " + c.Name + ": enum needs options"
		}
		if c.Name == "" || c.Label == "" {
			return "column name and label are required"
		}
		if _, err := ds.QuoteIdent(c.Name); err != nil {
			return "invalid identifier: " + c.Name
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
	return ""
}

// m2mConfig is the resolved many-to-many configuration of one column.
type m2mConfig struct {
	Junction  *meta.TableDef
	JCols     []meta.ColumnDef
	SrcCol    *meta.ColumnDef // junction fk column → this table
	TgtCol    *meta.ColumnDef // junction fk column → target table
	TargetID  int64           // target def id (from TgtCol.FKTableDefID)
	TargetRef string          // target ref column (TgtCol.FKRefColumn)
	SrcRef    string          // this table's column the junction references
}

// resolveM2M loads and cross-checks one column's m2m payload. Returns nil
// config + error message on any inconsistency.
func (s *Server) resolveM2M(def *meta.TableDef, c meta.ColumnDef) (*m2mConfig, string) {
	jdef, jcols, err := s.store.GetTableDef(c.M2MJunctionDefID)
	if err != nil {
		return nil, "column " + c.Name + ": junction definition not found (save it first)"
	}
	if jdef.ID == def.ID {
		return nil, "column " + c.Name + ": junction cannot be this table itself"
	}
	var src, tgt *meta.ColumnDef
	for i, jc := range jcols {
		if jc.Name == c.M2MJunctionSrcCol && jc.FieldType == "fk" {
			src = &jcols[i]
		}
		if jc.Name == c.M2MJunctionTgtCol && jc.FieldType == "fk" {
			tgt = &jcols[i]
		}
	}
	if src == nil || tgt == nil {
		return nil, "column " + c.Name + ": junction source/target columns must be defined fk columns"
	}
	if src.Name == tgt.Name {
		return nil, "column " + c.Name + ": junction source and target columns must differ"
	}
	if src.FKTableDefID != def.ID {
		return nil, "column " + c.Name + ": junction source column must reference this table"
	}
	// every required junction column must be one of the two link columns —
	// otherwise link inserts would violate NOT NULL
	for _, jc := range jcols {
		if jc.Required && jc.Name != src.Name && jc.Name != tgt.Name {
			return nil, "column " + c.Name + ": junction has required column " + jc.Name +
				" outside the two link columns"
		}
	}
	return &m2mConfig{Junction: jdef, JCols: jcols, SrcCol: src, TgtCol: tgt,
		TargetID: tgt.FKTableDefID, TargetRef: tgt.FKRefColumn, SrcRef: src.FKRefColumn}, ""
}

// validateM2M checks one column's m2m payload (mirror of validateFK).
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
	cfg, msg := s.resolveM2M(def, c)
	if cfg == nil {
		return msg
	}
	_, tcols, err := s.store.GetTableDef(cfg.TargetID)
	if err != nil {
		return "column " + c.Name + ": m2m target definition not found"
	}
	names := map[string]bool{}
	for _, t := range tcols {
		names[t.Name] = true
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
	if err := s.store.SaveTableDef(def, cols); err != nil {
		writeErr(w, 400, "VALIDATION", "save failed", err.Error())
		return
	}
	writeJSON(w, 200, s.toTableDTO(def, cols, permsDTO{true, true, true, true}))
}

func (s *Server) handleTableList(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	list, err := s.store.ListTableDefs()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	out := []tableDefDTO{}
	for i := range list {
		p := s.tablePerms(u, list[i].ID)
		// Platform users see every definition (they manage them); everyone
		// else only sees tables they can read. The permissions object always
		// reflects actual row-CRUD grants.
		if !u.PlatformManage && !p.Read {
			continue
		}
		out = append(out, s.toTableDTO(&list[i], nil, p))
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

func fieldTypeOf(cols []meta.ColumnDef, name string) string {
	for _, c := range cols {
		if c.Name == name {
			return c.FieldType
		}
	}
	return "text"
}

func (s *Server) handleTableGet(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	p := s.tablePerms(u, def.ID)
	if !u.PlatformManage && !p.Read {
		writeErr(w, 403, "FORBIDDEN", "no access to this table", nil)
		return
	}
	writeJSON(w, 200, s.toTableDTO(def, cols, p))
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
	if msg := s.validateDef(def, cols); msg != "" {
		writeErr(w, 400, "VALIDATION", msg, nil)
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
	writeJSON(w, 200, s.toTableDTO(def, cols, permsDTO{true, true, true, true}))
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
	a, err := ds.Open(*d)
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
	a, err := ds.Open(*d)
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
	live, err := db.InspectTable(def.SchemaName, def.TableName)
	if err != nil {
		writeErr(w, 502, "CONN", "introspection failed", err.Error())
		return
	}
	rep := ds.CompareDrift(cols, live)
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
	live, err := db.InspectTable(def.SchemaName, def.TableName)
	if err != nil {
		writeErr(w, 502, "CONN", "introspection failed", err.Error())
		return
	}
	missing := ds.CompareDrift(cols, live).Missing
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
		if c.FieldType == "m2m" {
			out = append(out, c) // virtual relation column — preserved on resync
			continue
		}
		lc, ok := liveByName[c.Name]
		if !ok {
			continue // dropped
		}
		if ds.EffectiveType(c) != lc.FieldType {
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
	writeJSON(w, 200, s.toTableDTO(def, fresh, permsDTO{true, true, true, true}))
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
	a, err := ds.Open(*d)
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
