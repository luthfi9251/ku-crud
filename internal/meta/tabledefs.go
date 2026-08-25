package meta

import (
	"database/sql"
	"encoding/json"

	"ku-crud/internal/defs"
)

// SelfRef marks an FK target as "this table definition"; Save/Update rewrite
// it to the real def id before columns hit the store.
const SelfRef int64 = -1

type TableDef struct {
	ID             int64    `json:"id"`
	DatasourceID   int64    `json:"datasourceId"`
	SchemaName     string   `json:"schemaName"`
	TableName      string   `json:"tableName"`
	Label          string   `json:"label"`
	Description    string   `json:"description,omitempty"`
	KeyColumns     []string `json:"keyColumns"`
	PageSize       int      `json:"pageSize"`
	DefaultSortCol string   `json:"defaultSortCol"`
	DefaultSortDir string   `json:"defaultSortDir"`
	DefaultView    string   `json:"defaultView,omitempty"`
	ViewConfig     string   `json:"viewConfig,omitempty"`
	GroupID        int64    `json:"groupId,omitempty"`
	Hooks          string   `json:"hooks,omitempty"`
	Actions        string   `json:"actions,omitempty"`
	SourceType     string   `json:"sourceType,omitempty"` // "table" (default) | "query"
	QuerySQL       string   `json:"querySql,omitempty"`
}

func normalizeSource(d *TableDef) {
	if d.SourceType != "query" {
		d.SourceType = "table"
		d.QuerySQL = ""
	}
}

// ValidationRule is one optional per-column rule enforced on every write.
type ValidationRule struct {
	Type  string `json:"type"`            // email | min_len | max_len | number | text
	Param int    `json:"param,omitempty"` // bound for min_len / max_len
}

type ColumnDef struct {
	ID          int64            `json:"id"`
	TableDefID  int64            `json:"tableDefId"`
	Name        string           `json:"name"`
	Label       string           `json:"label"`
	FieldType   string           `json:"fieldType"`
	EnumOptions []string         `json:"enumOptions"`
	Editable    bool             `json:"editable"`
	Required    bool             `json:"required"`
	Visible     bool             `json:"visible"`
	Searchable  bool             `json:"searchable"`
	Sortable    bool             `json:"sortable"`
	Position    int              `json:"position"`
	Validations []ValidationRule `json:"validations,omitempty"`
	// Formatting is display-only JSON (never written to DB or export).
	Formatting       string   `json:"-"`
	IsComputed       bool     `json:"isComputed,omitempty"`
	ComputedFormula  string   `json:"computedFormula,omitempty"`
	BaseType         string   `json:"baseType,omitempty"`
	FKTableDefID     int64    `json:"fkTableDefId,omitempty"`
	FKRefColumn      string   `json:"fkRefColumn,omitempty"`
	FKDisplayColumns []string `json:"fkDisplayColumns,omitempty"`
	// m2m virtual columns (fieldType == "m2m") — no live column counterpart.
	M2MJunctionDefID  int64    `json:"m2mJunctionDefId,omitempty"`
	M2MJunctionSrcCol string   `json:"m2mJunctionSrcCol,omitempty"`
	M2MJunctionTgtCol string   `json:"m2mJunctionTgtCol,omitempty"`
	M2MDisplayColumns []string `json:"m2mDisplayColumns,omitempty"`
}

func resolveSelfRefs(defID int64, cols []ColumnDef) {
	for i := range cols {
		if cols[i].FKTableDefID == SelfRef {
			cols[i].FKTableDefID = defID
		}
	}
}

// ToCoreDef converts a persisted definition into the ID-free core contract.
// FK and M2M links become definition names via idToName; a self-FK targets "".
func ToCoreDef(md TableDef, cols []ColumnDef, idToName map[int64]string) defs.Table {
	t := defs.Table{
		Name:           md.TableName,
		Label:          md.Label,
		Description:    md.Description,
		Schema:         md.SchemaName,
		PhysTab:        md.TableName,
		Keys:           md.KeyColumns,
		PageSize:       md.PageSize,
		DefaultSortCol: md.DefaultSortCol,
		DefaultSortDir: md.DefaultSortDir,
		DefaultView:    md.DefaultView,
		ViewConfig:     md.ViewConfig,
		SourceType:     md.SourceType,
		QuerySQL:       md.QuerySQL,
		Hooks:          md.Hooks,
		Actions:        md.Actions,
		Columns:        make([]defs.Column, 0, len(cols)),
	}
	for _, c := range cols {
		dc := defs.Column{
			Name:            c.Name,
			Label:           c.Label,
			FieldType:       c.FieldType,
			EnumOptions:     c.EnumOptions,
			Editable:        c.Editable,
			Required:        c.Required,
			Visible:         c.Visible,
			Searchable:      c.Searchable,
			Sortable:        c.Sortable,
			Position:        c.Position,
			Validations:     toCoreValidations(c.Validations),
			Formatting:      c.Formatting,
			IsComputed:      c.IsComputed,
			ComputedFormula: c.ComputedFormula,
			BaseType:        c.BaseType,
		}
		if c.FieldType == "fk" && c.FKTableDefID > 0 {
			fk := &defs.FK{RefColumn: c.FKRefColumn, DisplayColumns: c.FKDisplayColumns}
			if c.FKTableDefID != md.ID {
				fk.Table = idToName[c.FKTableDefID]
			}
			dc.FK = fk
		}
		if c.FieldType == "m2m" && c.M2MJunctionDefID > 0 {
			dc.M2M = &defs.M2M{
				JunctionTable:  idToName[c.M2MJunctionDefID],
				SrcCol:         c.M2MJunctionSrcCol,
				TgtCol:         c.M2MJunctionTgtCol,
				DisplayColumns: c.M2MDisplayColumns,
			}
		}
		t.Columns = append(t.Columns, dc)
	}
	return t
}

func toCoreValidations(vs []ValidationRule) []defs.ValidationRule {
	if len(vs) == 0 {
		return nil
	}
	out := make([]defs.ValidationRule, len(vs))
	for i, v := range vs {
		out[i] = defs.ValidationRule{Type: v.Type, Param: v.Param}
	}
	return out
}

func insertCols(tx *sql.Tx, defID int64, cols []ColumnDef) error {
	for _, c := range cols {
		var opts any
		if len(c.EnumOptions) > 0 {
			b, _ := json.Marshal(c.EnumOptions)
			opts = string(b)
		}
		var disp any
		if len(c.FKDisplayColumns) > 0 {
			b, _ := json.Marshal(c.FKDisplayColumns)
			disp = string(b)
		}
		m2mDisp := ""
		if len(c.M2MDisplayColumns) > 0 {
			b, _ := json.Marshal(c.M2MDisplayColumns)
			m2mDisp = string(b)
		}
		vRules := ""
		if len(c.Validations) > 0 {
			b, _ := json.Marshal(c.Validations)
			vRules = string(b)
		}
		_, err := tx.Exec(`INSERT INTO columns(table_def_id,name,label,field_type,enum_options,
			editable,required,visible,searchable,sortable,position,
			base_type,fk_table_def_id,fk_ref_column,fk_display_columns,
			m2m_junction_def_id,m2m_junction_src_col,m2m_junction_tgt_col,m2m_display_cols,validations,
			formatting,is_computed,computed_formula)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			defID, c.Name, c.Label, c.FieldType, opts,
			c.Editable, c.Required, c.Visible, c.Searchable, c.Sortable, c.Position,
			c.BaseType, c.FKTableDefID, c.FKRefColumn, disp,
			c.M2MJunctionDefID, c.M2MJunctionSrcCol, c.M2MJunctionTgtCol, m2mDisp, vRules,
			c.Formatting, c.IsComputed, c.ComputedFormula)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveTableDef(def *TableDef, cols []ColumnDef) error {
	normalizeSource(def)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	kj, err := json.Marshal(def.KeyColumns)
	if err != nil {
		return err
	}
	if def.DefaultSortDir != "DESC" {
		def.DefaultSortDir = "ASC"
	}
	var gid any
	if def.GroupID > 0 {
		gid = def.GroupID
	}
	res, err := tx.Exec(`INSERT INTO table_defs(datasource_id,schema_name,table_name,label,description,key_columns,page_size,default_sort_col,default_sort_dir,default_view,view_config,group_id,hooks,actions,source_type,query_sql)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, def.DatasourceID, def.SchemaName, def.TableName, def.Label, def.Description, string(kj), def.PageSize, def.DefaultSortCol, def.DefaultSortDir, def.DefaultView, def.ViewConfig, gid, def.Hooks, def.Actions, def.SourceType, def.QuerySQL)
	if err != nil {
		tx.Rollback()
		return err
	}
	def.ID, _ = res.LastInsertId()
	resolveSelfRefs(def.ID, cols)
	if err := insertCols(tx, def.ID, cols); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateTableDef(def *TableDef, cols []ColumnDef) error {
	normalizeSource(def)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	kj, err := json.Marshal(def.KeyColumns)
	if err != nil {
		tx.Rollback()
		return err
	}
	if def.DefaultSortDir != "DESC" {
		def.DefaultSortDir = "ASC"
	}
	var gid any
	if def.GroupID > 0 {
		gid = def.GroupID
	}
	res, err := tx.Exec(`UPDATE table_defs SET datasource_id=?,schema_name=?,table_name=?,label=?,description=?,key_columns=?,page_size=?,default_sort_col=?,default_sort_dir=?,default_view=?,view_config=?,group_id=?,hooks=?,actions=?,source_type=?,query_sql=?
		WHERE id=?`, def.DatasourceID, def.SchemaName, def.TableName, def.Label, def.Description, string(kj), def.PageSize, def.DefaultSortCol, def.DefaultSortDir, def.DefaultView, def.ViewConfig, gid, def.Hooks, def.Actions, def.SourceType, def.QuerySQL, def.ID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		tx.Rollback()
		return ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM columns WHERE table_def_id=?`, def.ID); err != nil {
		tx.Rollback()
		return err
	}
	resolveSelfRefs(def.ID, cols)
	if err := insertCols(tx, def.ID, cols); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) ReplaceColumns(defID int64, cols []ColumnDef) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM columns WHERE table_def_id=?`, defID); err != nil {
		tx.Rollback()
		return err
	}
	if err := insertCols(tx, defID, cols); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) ListTableDefs() ([]TableDef, error) {
	rows, err := s.db.Query(`SELECT id,datasource_id,schema_name,table_name,label,description,key_columns,page_size,default_sort_col,default_sort_dir,default_view,view_config,group_id,hooks,actions,source_type,query_sql
		FROM table_defs ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TableDef
	for rows.Next() {
		var d TableDef
		var kj string
		var gid sql.NullInt64
		if err := rows.Scan(&d.ID, &d.DatasourceID, &d.SchemaName, &d.TableName, &d.Label, &d.Description, &kj, &d.PageSize, &d.DefaultSortCol, &d.DefaultSortDir, &d.DefaultView, &d.ViewConfig, &gid, &d.Hooks, &d.Actions, &d.SourceType, &d.QuerySQL); err != nil {
			return nil, err
		}
		d.GroupID = gid.Int64
		json.Unmarshal([]byte(kj), &d.KeyColumns)
		normalizeSource(&d)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetTableDef(id int64) (*TableDef, []ColumnDef, error) {
	d := &TableDef{}
	var kj string
	var gid sql.NullInt64
	err := s.db.QueryRow(`SELECT id,datasource_id,schema_name,table_name,label,description,key_columns,page_size,default_sort_col,default_sort_dir,default_view,view_config,group_id,hooks,actions,source_type,query_sql
		FROM table_defs WHERE id=?`, id).
		Scan(&d.ID, &d.DatasourceID, &d.SchemaName, &d.TableName, &d.Label, &d.Description, &kj, &d.PageSize, &d.DefaultSortCol, &d.DefaultSortDir, &d.DefaultView, &d.ViewConfig, &gid, &d.Hooks, &d.Actions, &d.SourceType, &d.QuerySQL)
	if err == sql.ErrNoRows {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	d.GroupID = gid.Int64
	json.Unmarshal([]byte(kj), &d.KeyColumns)
	normalizeSource(d)
	cols, err := s.getColumns(id)
	return d, cols, err
}

func (s *Store) getColumns(defID int64) ([]ColumnDef, error) {
	rows, err := s.db.Query(`SELECT id,table_def_id,name,label,field_type,enum_options,
		editable,required,visible,searchable,sortable,position,
		base_type,fk_table_def_id,fk_ref_column,fk_display_columns,
		m2m_junction_def_id,m2m_junction_src_col,m2m_junction_tgt_col,m2m_display_cols,validations,
		formatting,is_computed,computed_formula
		FROM columns WHERE table_def_id=? ORDER BY position`, defID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ColumnDef
	for rows.Next() {
		var c ColumnDef
		var opts, disp, m2mDisp, vld, fmtv sql.NullString
		if err := rows.Scan(&c.ID, &c.TableDefID, &c.Name, &c.Label, &c.FieldType, &opts,
			&c.Editable, &c.Required, &c.Visible, &c.Searchable, &c.Sortable, &c.Position,
			&c.BaseType, &c.FKTableDefID, &c.FKRefColumn, &disp,
			&c.M2MJunctionDefID, &c.M2MJunctionSrcCol, &c.M2MJunctionTgtCol, &m2mDisp, &vld,
			&fmtv, &c.IsComputed, &c.ComputedFormula); err != nil {
			return nil, err
		}
		if opts.Valid && opts.String != "" {
			json.Unmarshal([]byte(opts.String), &c.EnumOptions)
		}
		if disp.Valid && disp.String != "" {
			json.Unmarshal([]byte(disp.String), &c.FKDisplayColumns)
		}
		if m2mDisp.Valid && m2mDisp.String != "" {
			json.Unmarshal([]byte(m2mDisp.String), &c.M2MDisplayColumns)
		}
		if vld.Valid && vld.String != "" {
			json.Unmarshal([]byte(vld.String), &c.Validations)
		}
		c.Formatting = fmtv.String
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteTableDef(id int64) error {
	res, err := s.db.Exec(`DELETE FROM table_defs WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FKSource describes one defined FK column pointing at defID.
type FKSource struct {
	DefID     int64
	DefLabel  string
	Column    string
	RefColumn string
}

func (s *Store) FKRefSources(defID int64) ([]FKSource, error) {
	rows, err := s.db.Query(`SELECT td.id, td.label, c.name, c.fk_ref_column
		FROM columns c JOIN table_defs td ON td.id = c.table_def_id
		WHERE c.fk_table_def_id = ? AND c.field_type = 'fk'`, defID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FKSource
	for rows.Next() {
		var f FKSource
		if err := rows.Scan(&f.DefID, &f.DefLabel, &f.Column, &f.RefColumn); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
