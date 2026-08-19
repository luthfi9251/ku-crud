package api

import (
	"database/sql"
	"errors"
	"net/http"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

// tableDefInput accepts masked datasource ids from the client.
type tableDefInput struct {
	DatasourceID string           `json:"datasourceId"`
	SchemaName   string           `json:"schemaName"`
	TableName    string           `json:"tableName"`
	Label        string           `json:"label"`
	KeyColumns   []string         `json:"keyColumns"`
	PageSize     int              `json:"pageSize"`
	Columns      []meta.ColumnDef `json:"columns"`
}

func (s *Server) toDef(in tableDefInput) (*meta.TableDef, error) {
	dsID, err := s.ids.Decode("ds", in.DatasourceID)
	if err != nil {
		return nil, errors.New("invalid datasourceId")
	}
	return &meta.TableDef{DatasourceID: dsID, SchemaName: in.SchemaName,
		TableName: in.TableName, Label: in.Label, KeyColumns: in.KeyColumns,
		PageSize: in.PageSize}, nil
}

type permsDTO struct {
	Read   bool `json:"read"`
	Create bool `json:"create"`
	Update bool `json:"update"`
	Delete bool `json:"delete"`
}

type columnDTO struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	FieldType   string   `json:"fieldType"`
	EnumOptions []string `json:"enumOptions"`
	Editable    bool     `json:"editable"`
	Required    bool     `json:"required"`
	Visible     bool     `json:"visible"`
	Searchable  bool     `json:"searchable"`
	Sortable    bool     `json:"sortable"`
	Position    int      `json:"position"`
}

func colToDTO(c meta.ColumnDef) columnDTO {
	return columnDTO{c.Name, c.Label, c.FieldType, c.EnumOptions,
		c.Editable, c.Required, c.Visible, c.Searchable, c.Sortable, c.Position}
}

// tableDefDTO masks ids and carries the caller's grants.
type tableDefDTO struct {
	ID           string      `json:"id"`
	DatasourceID string      `json:"datasourceId"`
	SchemaName   string      `json:"schemaName"`
	TableName    string      `json:"tableName"`
	Label        string      `json:"label"`
	KeyColumns   []string    `json:"keyColumns"`
	PageSize     int         `json:"pageSize"`
	Columns      []columnDTO `json:"columns,omitempty"`
	Permissions  permsDTO    `json:"permissions"`
}

func (s *Server) toTableDTO(def *meta.TableDef, cols []meta.ColumnDef, p permsDTO) tableDefDTO {
	dto := tableDefDTO{
		ID:           s.ids.Encode("td", def.ID),
		DatasourceID: s.ids.Encode("ds", def.DatasourceID),
		SchemaName:   def.SchemaName,
		TableName:    def.TableName,
		Label:        def.Label,
		KeyColumns:   def.KeyColumns,
		PageSize:     def.PageSize,
		Permissions:  p,
	}
	if dto.KeyColumns == nil {
		dto.KeyColumns = []string{}
	}
	for _, c := range cols {
		dto.Columns = append(dto.Columns, colToDTO(c))
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
}

var (
	errDSNotFound = errors.New("datasource not found")
	errConn       = errors.New("connection failed")
)

func validateDef(def *meta.TableDef, cols []meta.ColumnDef) string {
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
	for i, c := range cols {
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
		for k, key := range def.KeyColumns {
			if c.Name == key {
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
	if msg := validateDef(def, in.Columns); msg != "" {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	if err := s.store.SaveTableDef(def, in.Columns); err != nil {
		writeErr(w, 400, "VALIDATION", "save failed", err.Error())
		return
	}
	writeJSON(w, 200, s.toTableDTO(def, in.Columns, permsDTO{true, true, true, true}))
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
	if msg := validateDef(def, in.Columns); msg != "" {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	def.ID = id
	if err := s.store.UpdateTableDef(def, in.Columns); err != nil {
		if errors.Is(err, meta.ErrNotFound) {
			writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
			return
		}
		writeErr(w, 400, "VALIDATION", "update failed", err.Error())
		return
	}
	writeJSON(w, 200, s.toTableDTO(def, in.Columns, permsDTO{true, true, true, true}))
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

// liveDB opens the live PG connection for a datasource id.
func (s *Server) liveDB(dsID int64) (*sql.DB, error) {
	d, err := s.store.GetDatasource(dsID)
	if errors.Is(err, meta.ErrNotFound) {
		return nil, errDSNotFound
	}
	if err != nil {
		return nil, err
	}
	db, err := ds.Connect(dsToDSN(d))
	if err != nil {
		return nil, errConn
	}
	return db, nil
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
	db, err := s.liveDB(id)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()
	tables, err := ds.ListTables(db)
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
	db, err := s.liveDB(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()
	live, err := ds.InspectTable(db, def.SchemaName, def.TableName)
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
	db, err := s.liveDB(def.DatasourceID)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()
	live, err := ds.InspectTable(db, def.SchemaName, def.TableName)
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
		lc, ok := liveByName[c.Name]
		if !ok {
			continue // dropped
		}
		if c.FieldType != lc.FieldType {
			c.FieldType = lc.FieldType
			if lc.FieldType == "enum" {
				c.EnumOptions = lc.EnumOptions
			} else {
				c.EnumOptions = nil
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
	db, err := s.liveDB(id)
	if err != nil {
		s.writeLiveErr(w, err)
		return
	}
	defer db.Close()
	cols, err := ds.InspectTable(db, r.PathValue("schema"), r.PathValue("table"))
	if err != nil {
		writeErr(w, 502, "CONN", "introspection failed", err.Error())
		return
	}
	if cols == nil {
		cols = []ds.LiveColumn{}
	}
	writeJSON(w, 200, cols)
}
