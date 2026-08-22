package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

const importMetaMaxFile = 2 << 20 // 2 MB — metadata files are small

type importPreviewRes struct {
	Datasources []dsPreviewItem  `json:"datasources"`
	Tables      []tblPreviewItem `json:"tables"`
}

type dsPreviewItem struct {
	Ref       string   `json:"ref"`    // datasource name
	Status    string   `json:"status"` // new | duplicate-identical | duplicate-conflicts
	Conflicts []string `json:"conflicts,omitempty"`
}

type tblPreviewItem struct {
	Ref          string    `json:"ref"` // "<datasourceRef>/<schema>/<table>"
	Status       string    `json:"status"`
	Dependencies []depItem `json:"dependencies"`
	Invalid      bool      `json:"invalid,omitempty"`
	Reason       string    `json:"reason,omitempty"`
}

type depItem struct {
	Ref      string `json:"ref"`
	Resolved bool   `json:"resolved"` // exists locally or inside the file
}

type metaTableRef struct {
	DatasourceRef string `json:"datasourceRef"`
	Schema        string `json:"schema"`
	Table         string `json:"table"`
}

type metaFileColumn struct {
	Name        string                `json:"name"`
	Label       string                `json:"label"`
	FieldType   string                `json:"fieldType"`
	EnumOptions []string              `json:"enumOptions"`
	Editable    bool                  `json:"editable"`
	Required    bool                  `json:"required"`
	Visible     bool                  `json:"visible"`
	Searchable  bool                  `json:"searchable"`
	Sortable    bool                  `json:"sortable"`
	Position    int                   `json:"position"`
	BaseType    string                `json:"baseType,omitempty"`
	Validations []meta.ValidationRule `json:"validations,omitempty"`
	FKTableRef  *metaTableRef         `json:"fkTableRef"`
	FKRefColumn string                `json:"fkRefColumn,omitempty"`
	FKDisplay   []string              `json:"fkDisplayColumns"`
	M2MJunction *metaTableRef         `json:"m2mJunctionTableRef"`
	M2MSrcCol   string                `json:"m2mJunctionSrcCol,omitempty"`
	M2MTgtCol   string                `json:"m2mJunctionTgtCol,omitempty"`
	M2MDisplay  []string              `json:"m2mDisplayColumns"`
	IsComputed  bool                  `json:"isComputed,omitempty"`
	ComputedFml string                `json:"computedFormula,omitempty"`
	Formatting  json.RawMessage       `json:"formatting,omitempty"`
}

type metaFileTable struct {
	DatasourceRef  string           `json:"datasourceRef"`
	Schema         string           `json:"schema"`
	Table          string           `json:"table"`
	Label          string           `json:"label"`
	Description    string           `json:"description,omitempty"`
	KeyColumns     []string         `json:"keyColumns"`
	PageSize       int              `json:"pageSize"`
	DefaultSortCol string           `json:"defaultSortCol"`
	DefaultSortDir string           `json:"defaultSortDir"`
	DefaultView    string           `json:"defaultView,omitempty"`
	ViewConfig     json.RawMessage  `json:"viewConfig,omitempty"`
	Hooks          json.RawMessage  `json:"hooks,omitempty"`
	GroupRef       string           `json:"groupRef,omitempty"`
	Columns        []metaFileColumn `json:"columns"`
}

type metaFileDatasource struct {
	Name     string `json:"name"`
	Adapter  string `json:"adapter"` // driver
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	SSLMode  string `json:"sslMode"`
	// NOTE: no password field — never exported (spec §3.2)
}

type metaFile struct {
	Format      string               `json:"format"` // "ku-crud-meta"
	Version     int                  `json:"version"`
	ExportedAt  string               `json:"exportedAt"`
	Groups      []string             `json:"groups"`
	Datasources []metaFileDatasource `json:"datasources"`
	Tables      []metaFileTable      `json:"tables"`
}

// nonNil normalizes a nil slice to an empty one so the exported JSON carries
// [] instead of null (stable file format for Tasks 10-12).
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilRules(s []meta.ValidationRule) []meta.ValidationRule {
	if s == nil {
		return []meta.ValidationRule{}
	}
	return s
}

func (s *Server) buildMetaFile() (*metaFile, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	f := &metaFile{Format: "ku-crud-meta", Version: 1, ExportedAt: now,
		Groups: []string{}, Datasources: []metaFileDatasource{}, Tables: []metaFileTable{}}

	gs, err := s.store.ListGroups()
	if err != nil {
		return nil, err
	}
	groupName := map[int64]string{}
	for _, g := range gs {
		f.Groups = append(f.Groups, g.Name)
		groupName[g.ID] = g.Name
	}

	dss, err := s.store.ListDatasources()
	if err != nil {
		return nil, err
	}
	dsName := map[int64]string{}
	for _, d := range dss {
		dsName[d.ID] = d.Name
		f.Datasources = append(f.Datasources, metaFileDatasource{
			Name: d.Name, Adapter: d.Driver, Host: d.Host, Port: d.Port,
			Database: d.DBName, User: d.Username, SSLMode: d.SSLMode,
		})
	}

	defs, err := s.store.ListTableDefs()
	if err != nil {
		return nil, err
	}
	type nk struct{ ds, schema, table string }
	defKey := map[int64]nk{}
	for _, d := range defs {
		defKey[d.ID] = nk{dsName[d.DatasourceID], d.SchemaName, d.TableName}
	}
	refOf := func(id int64) *metaTableRef {
		k, ok := defKey[id]
		if !ok {
			return nil
		}
		return &metaTableRef{DatasourceRef: k.ds, Schema: k.schema, Table: k.table}
	}
	for _, d := range defs {
		_, cols, err := s.store.GetTableDef(d.ID)
		if err != nil {
			return nil, err
		}
		ft := metaFileTable{DatasourceRef: dsName[d.DatasourceID], Schema: d.SchemaName,
			Table: d.TableName, Label: d.Label, Description: d.Description,
			KeyColumns: nonNil(d.KeyColumns), PageSize: d.PageSize,
			DefaultSortCol: d.DefaultSortCol, DefaultSortDir: d.DefaultSortDir,
			DefaultView: d.DefaultView,
			GroupRef:    groupName[d.GroupID], Columns: []metaFileColumn{}}
		if d.ViewConfig != "" {
			ft.ViewConfig = json.RawMessage(d.ViewConfig)
		}
		if d.Hooks != "" {
			ft.Hooks = json.RawMessage(d.Hooks)
		}
		for _, c := range cols {
			fc := metaFileColumn{Name: c.Name, Label: c.Label, FieldType: c.FieldType,
				EnumOptions: nonNil(c.EnumOptions), Editable: c.Editable, Required: c.Required,
				Visible: c.Visible, Searchable: c.Searchable, Sortable: c.Sortable,
				Position: c.Position, BaseType: c.BaseType, Validations: nonNilRules(c.Validations),
				FKRefColumn: c.FKRefColumn, FKDisplay: nonNil(c.FKDisplayColumns),
				M2MSrcCol: c.M2MJunctionSrcCol, M2MTgtCol: c.M2MJunctionTgtCol, M2MDisplay: nonNil(c.M2MDisplayColumns),
				IsComputed: c.IsComputed, ComputedFml: c.ComputedFormula}
			if c.FKTableDefID > 0 {
				fc.FKTableRef = refOf(c.FKTableDefID)
			}
			if c.M2MJunctionDefID > 0 {
				fc.M2MJunction = refOf(c.M2MJunctionDefID)
			}
			if c.Formatting != "" {
				fc.Formatting = json.RawMessage(c.Formatting)
			}
			ft.Columns = append(ft.Columns, fc)
		}
		f.Tables = append(f.Tables, ft)
	}
	return f, nil
}

func (s *Server) handleMetaExport(w http.ResponseWriter, r *http.Request) {
	f, err := s.buildMetaFile()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="ku-crud-meta-%s.json"`, time.Now().Format("20060102-150405")))
	writeJSON(w, 200, f)
}

// parseMetaFile validates structure/format/version.
func parseMetaFile(body []byte) (*metaFile, string) {
	var f metaFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, "file is not valid JSON: " + err.Error()
	}
	if f.Format != "ku-crud-meta" {
		return nil, `file field "format" must be "ku-crud-meta"`
	}
	if f.Version != 1 {
		return nil, "unsupported file version " + strconv.Itoa(f.Version)
	}
	if len(f.Tables) == 0 && len(f.Datasources) == 0 {
		return nil, "file contains no datasources or tables"
	}
	return &f, ""
}

func dsEqual(a metaFileDatasource, d meta.Datasource) bool {
	return a.Name == d.Name && a.Adapter == d.Driver && a.Host == d.Host &&
		a.Port == d.Port && a.Database == d.DBName && a.User == d.Username && a.SSLMode == d.SSLMode
}

// tableRef builds the "<ds>/<schema>/<table>" display ref.
func tableRef(ds, schema, table string) string { return ds + "/" + schema + "/" + table }

func tblEqual(ft metaFileTable, def *meta.TableDef, cols []meta.ColumnDef, dsName, groupName string) bool {
	if ft.DatasourceRef != dsName || ft.Schema != def.SchemaName || ft.Table != def.TableName ||
		ft.Label != def.Label || ft.Description != def.Description || ft.PageSize != def.PageSize ||
		ft.DefaultSortCol != def.DefaultSortCol || ft.DefaultSortDir != def.DefaultSortDir ||
		ft.DefaultView != def.DefaultView || string(ft.ViewConfig) != def.ViewConfig ||
		string(ft.Hooks) != def.Hooks ||
		ft.GroupRef != groupName || len(ft.KeyColumns) != len(def.KeyColumns) {
		return false
	}
	for i := range ft.KeyColumns {
		if ft.KeyColumns[i] != def.KeyColumns[i] {
			return false
		}
	}
	if len(ft.Columns) != len(cols) {
		return false
	}
	for i, c := range cols {
		fc := ft.Columns[i]
		if fc.Name != c.Name || fc.Label != c.Label || fc.FieldType != c.FieldType ||
			fc.Editable != c.Editable || fc.Required != c.Required || fc.Visible != c.Visible ||
			fc.Searchable != c.Searchable || fc.Sortable != c.Sortable || fc.Position != c.Position ||
			fc.IsComputed != c.IsComputed || fc.ComputedFml != c.ComputedFormula ||
			string(fc.Formatting) != c.Formatting ||
			len(fc.Validations) != len(c.Validations) {
			return false
		}
	}
	return true
}

func (s *Server) handleMetaImportPreview(w http.ResponseWriter, r *http.Request) {
	file, msg := readMetaUpload(r)
	if msg != "" {
		writeErr(w, 400, "META_FILE_INVALID", msg, nil)
		return
	}
	f, msg := parseMetaFile(file)
	if msg != "" {
		writeErr(w, 400, "META_FILE_INVALID", msg, nil)
		return
	}
	res, msg := s.diffMeta(f)
	if msg != "" {
		// file already parsed OK — any failure here is a server fault
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, res)
}

func readMetaUpload(r *http.Request) ([]byte, string) {
	r.Body = http.MaxBytesReader(nil, r.Body, importMetaMaxFile)
	f, _, err := r.FormFile("file")
	if err != nil {
		return nil, "multipart field 'file' is required (max 2 MB)"
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, "read failed"
	}
	return b, ""
}

// diffMeta compares the file against local metadata and flags dependencies.
// Also used (partially) by apply for re-validation.
func (s *Server) diffMeta(f *metaFile) (*importPreviewRes, string) {
	res := &importPreviewRes{Datasources: []dsPreviewItem{}, Tables: []tblPreviewItem{}}

	localDS := map[string]meta.Datasource{}
	dss, err := s.store.ListDatasources()
	if err != nil {
		return nil, "list datasources failed: " + err.Error()
	}
	for _, d := range dss {
		localDS[d.Name] = d
	}
	fileTables := map[string]bool{} // "ds/schema/table" present in file
	for _, ft := range f.Tables {
		fileTables[tableRef(ft.DatasourceRef, ft.Schema, ft.Table)] = true
	}
	localTables := map[string]*meta.TableDef{}
	defs, err := s.store.ListTableDefs()
	if err != nil {
		return nil, "list table defs failed: " + err.Error()
	}
	groupName := s.groupNameMap()
	dsName := map[int64]string{}
	for _, d := range dss {
		dsName[d.ID] = d.Name
	}
	for i := range defs {
		d := &defs[i]
		localTables[tableRef(dsName[d.DatasourceID], d.SchemaName, d.TableName)] = d
	}

	for _, fds := range f.Datasources {
		item := dsPreviewItem{Ref: fds.Name, Status: "new"}
		if loc, ok := localDS[fds.Name]; ok {
			if dsEqual(fds, loc) {
				item.Status = "duplicate-identical"
			} else {
				item.Status = "duplicate-conflicts"
			}
		}
		res.Datasources = append(res.Datasources, item)
	}

	for _, ft := range f.Tables {
		ref := tableRef(ft.DatasourceRef, ft.Schema, ft.Table)
		item := tblPreviewItem{Ref: ref, Status: "new", Dependencies: []depItem{}}
		if loc, ok := localTables[ref]; ok {
			_, cols, err := s.store.GetTableDef(loc.ID)
			if err == nil && tblEqual(ft, loc, cols, ft.DatasourceRef, groupName[loc.GroupID]) {
				item.Status = "duplicate-identical"
			} else {
				item.Status = "duplicate-conflicts"
			}
		}
		// dependency refs: fkTableRef + m2mJunctionTableRef of every column
		for _, c := range ft.Columns {
			for _, refPtr := range []*metaTableRef{c.FKTableRef, c.M2MJunction} {
				if refPtr == nil {
					continue
				}
				dRef := tableRef(refPtr.DatasourceRef, refPtr.Schema, refPtr.Table)
				_, inFile := fileTables[dRef]
				_, local := localTables[dRef]
				resolved := inFile || local
				item.Dependencies = append(item.Dependencies, depItem{Ref: dRef, Resolved: resolved})
				if !resolved {
					item.Invalid = true
					item.Reason = "dependency " + dRef + " is neither in the file nor defined locally"
				}
			}
		}
		if item.Invalid {
			item.Status = "invalid-dependency"
		}
		res.Tables = append(res.Tables, item)
	}
	return res, ""
}

type applySelectionDS struct {
	Ref      string `json:"ref"`
	Mode     string `json:"mode"` // skip | overwrite
	Password string `json:"password"`
}
type applySelectionTable struct {
	Ref  string `json:"ref"`
	Mode string `json:"mode"` // skip | overwrite
}
type applySelections struct {
	Datasources []applySelectionDS    `json:"datasources"`
	Tables      []applySelectionTable `json:"tables"`
	Groups      bool                  `json:"groups"`
}

func (s *Server) handleMetaImportApply(w http.ResponseWriter, r *http.Request) {
	body, msg := readMetaUpload(r)
	if msg != "" {
		writeErr(w, 400, "META_FILE_INVALID", msg, nil)
		return
	}
	f, msg := parseMetaFile(body)
	if msg != "" {
		writeErr(w, 400, "META_FILE_INVALID", msg, nil)
		return
	}
	var sel applySelections
	if err := json.Unmarshal([]byte(r.FormValue("selections")), &sel); err != nil {
		writeErr(w, 400, "META_IMPORT_INVALID", "selections must be JSON", nil)
		return
	}
	plan, msg, err := s.buildImportPlan(f, sel)
	if err != nil {
		// store read failed — server fault; never reach ApplyImport (it would
		// silently create duplicates when overwrite-mode LocalID resolution
		// finds nothing in an empty def list)
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	if msg != "" {
		writeErr(w, 400, "META_IMPORT_INVALID", msg, nil)
		return
	}
	created, updated, err := s.store.ApplyImport(*plan)
	if err != nil {
		writeErr(w, 400, "META_IMPORT_INVALID", "import failed: "+err.Error(), nil)
		return
	}
	s.auditBestEffort(userFrom(r), 0, "meta_import", "",
		nil, map[string]int{"createdDefs": created, "updatedDefs": updated, "groupsCreated": len(plan.Groups)})
	writeJSON(w, 200, map[string]any{"createdDefs": created, "updatedDefs": updated, "groupsCreated": len(plan.Groups)})
}

// buildImportPlan turns the parsed file + user selections into an ImportPlan.
// The multipart body is fully parsed by readMetaUpload before FormValue is
// read, so selections lookup below is safe. Validation rejections come back
// as msg (caller maps to 400 META_IMPORT_INVALID); store read failures come
// back as err (caller maps to 500 INTERNAL, mirroring diffMeta's split).
func (s *Server) buildImportPlan(f *metaFile, sel applySelections) (*meta.ImportPlan, string, error) {
	plan := &meta.ImportPlan{Datasources: []meta.PlannedDatasource{}, Defs: []meta.PlannedDef{}}
	if sel.Groups {
		plan.Groups = f.Groups
	}

	localDS := map[string]bool{}
	dss, err := s.store.ListDatasources()
	if err != nil {
		return nil, "", fmt.Errorf("list datasources failed: %w", err)
	}
	for _, d := range dss {
		localDS[d.Name] = true
	}
	selDS := map[string]applySelectionDS{}
	for _, x := range sel.Datasources {
		selDS[x.Ref] = x
	}
	for _, fds := range f.Datasources {
		x, ok := selDS[fds.Name]
		if !ok || x.Mode == "skip" {
			continue
		}
		if !localDS[fds.Name] && x.Password == "" {
			return nil, "datasource " + fds.Name + " is new — a password is required for it", nil
		}
		plan.Datasources = append(plan.Datasources, meta.PlannedDatasource{
			DS: meta.Datasource{Name: fds.Name, Host: fds.Host, Port: fds.Port,
				DBName: fds.Database, Username: fds.User, SSLMode: fds.SSLMode, Driver: fds.Adapter},
			Password: x.Password,
		})
	}

	// index file tables + selected refs for dependency resolution
	fileTables := map[string]metaFileTable{}
	for _, ft := range f.Tables {
		fileTables[tableRef(ft.DatasourceRef, ft.Schema, ft.Table)] = ft
	}
	localRefs := map[string]bool{}
	defs, err := s.store.ListTableDefs()
	if err != nil {
		return nil, "", fmt.Errorf("list table defs failed: %w", err)
	}
	dsName := map[int64]string{}
	for _, d := range dss {
		dsName[d.ID] = d.Name
	}
	for _, d := range defs {
		localRefs[tableRef(dsName[d.DatasourceID], d.SchemaName, d.TableName)] = true
	}
	selTbl := map[string]string{}
	for _, x := range sel.Tables {
		selTbl[x.Ref] = x.Mode
	}

	for _, ft := range f.Tables {
		ref := tableRef(ft.DatasourceRef, ft.Schema, ft.Table)
		mode, ok := selTbl[ref]
		if !ok || mode == "skip" {
			continue
		}
		// dependency check: every fk/m2m ref must be local or selected
		for _, c := range ft.Columns {
			for _, rp := range []*metaTableRef{c.FKTableRef, c.M2MJunction} {
				if rp == nil {
					continue
				}
				dRef := tableRef(rp.DatasourceRef, rp.Schema, rp.Table)
				if !localRefs[dRef] {
					if _, inFile := fileTables[dRef]; !inFile {
						return nil, "table " + ref + " depends on " + dRef + " which is neither local nor in the file", nil
					}
					if _, selected := selTbl[dRef]; !selected {
						return nil, "table " + ref + " depends on " + dRef + " — select that table too", nil
					}
				}
			}
		}
		// datasource of this table must exist locally or be selected
		if !localDS[ft.DatasourceRef] {
			if _, selected := selDS[ft.DatasourceRef]; !selected {
				return nil, "table " + ref + " needs datasource " + ft.DatasourceRef + " — import it too", nil
			}
		}
		// bundle validation (structure — mirrors validateDef's core checks)
		if msg := validateBundleTable(ft); msg != "" {
			return nil, msg, nil
		}

		pd := meta.PlannedDef{DsName: ft.DatasourceRef, GroupName: ft.GroupRef,
			Def: meta.TableDef{SchemaName: ft.Schema, TableName: ft.Table, Label: ft.Label,
				Description: ft.Description,
				KeyColumns:  ft.KeyColumns, PageSize: ft.PageSize,
				DefaultSortCol: ft.DefaultSortCol, DefaultSortDir: ft.DefaultSortDir,
				DefaultView: ft.DefaultView, ViewConfig: string(ft.ViewConfig), Hooks: string(ft.Hooks)}}
		if mode == "overwrite" {
			for i := range defs {
				if tableRef(dsName[defs[i].DatasourceID], defs[i].SchemaName, defs[i].TableName) == ref {
					pd.LocalID = defs[i].ID
					break
				}
			}
		}
		for _, fc := range ft.Columns {
			pc := meta.PlannedColumn{Col: meta.ColumnDef{
				Name: fc.Name, Label: fc.Label, FieldType: fc.FieldType, EnumOptions: fc.EnumOptions,
				Editable: fc.Editable, Required: fc.Required, Visible: fc.Visible,
				Searchable: fc.Searchable, Sortable: fc.Sortable, Position: fc.Position,
				BaseType: fc.BaseType, Validations: fc.Validations,
				FKRefColumn: fc.FKRefColumn, FKDisplayColumns: fc.FKDisplay,
				M2MJunctionSrcCol: fc.M2MSrcCol, M2MJunctionTgtCol: fc.M2MTgtCol, M2MDisplayColumns: fc.M2MDisplay,
				IsComputed: fc.IsComputed, ComputedFormula: fc.ComputedFml,
				Formatting: string(fc.Formatting)}}
			if fc.FKTableRef != nil {
				pc.FKRef = meta.DefRef{DsName: fc.FKTableRef.DatasourceRef, Schema: fc.FKTableRef.Schema, Table: fc.FKTableRef.Table}
			}
			if fc.M2MJunction != nil {
				pc.M2MJRef = meta.DefRef{DsName: fc.M2MJunction.DatasourceRef, Schema: fc.M2MJunction.Schema, Table: fc.M2MJunction.Table}
			}
			pd.Columns = append(pd.Columns, pc)
		}
		plan.Defs = append(plan.Defs, pd)
	}
	return plan, "", nil
}

// validateBundleTable covers validateDef's structural core against a file
// table (identifier safety, keys exist, enum options, pageSize).
func validateBundleTable(ft metaFileTable) string {
	if ft.Schema == "" || ft.Table == "" || ft.Label == "" || len(ft.KeyColumns) == 0 {
		return "table " + ft.Table + ": schema/table/label/keyColumns are required"
	}
	if ft.PageSize < 1 || ft.PageSize > 200 {
		return "table " + ft.Table + ": pageSize must be 1..200"
	}
	for _, name := range append([]string{ft.Schema, ft.Table}, ft.KeyColumns...) {
		if _, err := ds.QuoteIdent(name); err != nil {
			return "table " + ft.Table + ": invalid identifier " + name
		}
	}
	// full column list (once) so computed formulas resolve their identifiers
	colDefs := make([]meta.ColumnDef, len(ft.Columns))
	for i, fc := range ft.Columns {
		colDefs[i] = meta.ColumnDef{Name: fc.Name, FieldType: fc.FieldType,
			IsComputed: fc.IsComputed, ComputedFormula: fc.ComputedFml}
	}
	names := map[string]bool{}
	for i, c := range ft.Columns {
		if c.Name == "" || c.Label == "" {
			return "table " + ft.Table + ": column name and label are required"
		}
		if _, err := ds.QuoteIdent(c.Name); err != nil {
			return "table " + ft.Table + ": invalid identifier " + c.Name
		}
		if !validFieldTypes[c.FieldType] {
			return "table " + ft.Table + ": column " + c.Name + " has invalid fieldType"
		}
		if c.FieldType == "enum" && len(c.EnumOptions) == 0 {
			return "table " + ft.Table + ": column " + c.Name + " enum needs options"
		}
		if c.IsComputed {
			if c.FieldType != "number" && c.FieldType != "text" {
				return "table " + ft.Table + ": column " + c.Name + ": computed columns must be number or text"
			}
			if c.Editable || c.Searchable || c.Sortable {
				return "table " + ft.Table + ": column " + c.Name + ": computed columns cannot be editable/searchable/sortable"
			}
			for _, key := range ft.KeyColumns {
				if c.Name == key {
					return "table " + ft.Table + ": column " + c.Name + ": computed columns cannot be key columns"
				}
			}
			if c.ComputedFml == "" {
				return "table " + ft.Table + ": column " + c.Name + ": computed columns need computedFormula"
			}
			ft2, _, err := compileComputed(colDefs[i], colDefs)
			if err != nil {
				return "table " + ft.Table + ": column " + c.Name + ": " + err.Error()
			}
			if ft2 != c.FieldType {
				return "table " + ft.Table + ": column " + c.Name + ": formula produces " + ft2 + " but the column type is " + c.FieldType
			}
		}
		names[c.Name] = true
	}
	for _, k := range ft.KeyColumns {
		if !names[k] {
			return "table " + ft.Table + ": key column " + k + " must be one of the columns"
		}
	}
	return ""
}
