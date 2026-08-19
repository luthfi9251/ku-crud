package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

type tableDefPayload struct {
	meta.TableDef
	Columns []meta.ColumnDef `json:"columns"`
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
		def.Label == "" || def.PKColumn == "" {
		return "datasourceId, schemaName, tableName, label, pkColumn are required"
	}
	if def.PageSize <= 0 || def.PageSize > 200 {
		return "pageSize must be 1..200"
	}
	for _, name := range []string{def.SchemaName, def.TableName, def.PKColumn} {
		if _, err := ds.QuoteIdent(name); err != nil {
			return "invalid identifier: " + name
		}
	}
	pkSeen := false
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
		if c.Name == def.PKColumn {
			pkSeen = true
		}
		for j := 0; j < i; j++ {
			if cols[j].Name == c.Name || cols[j].Position == c.Position {
				return "column names and positions must be unique"
			}
		}
	}
	if !pkSeen {
		return "pk column must be one of the defined columns"
	}
	return ""
}

func (s *Server) handleTableCreate(w http.ResponseWriter, r *http.Request) {
	var p tableDefPayload
	if err := readJSON(r, &p); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if msg := validateDef(&p.TableDef, p.Columns); msg != "" {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	if err := s.store.SaveTableDef(&p.TableDef, p.Columns); err != nil {
		writeErr(w, 400, "VALIDATION", "save failed", err.Error())
		return
	}
	writeJSON(w, 200, tableDefPayload{p.TableDef, p.Columns})
}

func (s *Server) handleTableList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTableDefs()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) tableCtx(r *http.Request) (*meta.TableDef, []meta.ColumnDef, error) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
	def, cols, err := s.tableCtx(r)
	if err != nil {
		s.writeDefErr(w, err)
		return
	}
	writeJSON(w, 200, tableDefPayload{*def, cols})
}

func (s *Server) handleTableUpdate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var p tableDefPayload
	if err := readJSON(r, &p); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	if msg := validateDef(&p.TableDef, p.Columns); msg != "" {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	p.ID = id
	if err := s.store.UpdateTableDef(&p.TableDef, p.Columns); err != nil {
		if errors.Is(err, meta.ErrNotFound) {
			writeErr(w, 404, "NOT_FOUND", "table def not found", nil)
			return
		}
		writeErr(w, 400, "VALIDATION", "update failed", err.Error())
		return
	}
	writeJSON(w, 200, tableDefPayload{p.TableDef, p.Columns})
}

func (s *Server) handleTableDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	err := s.store.DeleteTableDef(id)
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
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
	for _, m := range ds.CompareDrift(cols, live).Missing {
		if m == def.PKColumn {
			writeErr(w, 409, "DRIFT", "pk column was dropped; edit the definition manually", nil)
			return
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
	writeJSON(w, 200, tableDefPayload{*def, fresh})
}

func (s *Server) handleDSColumns(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
