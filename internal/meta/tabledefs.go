package meta

import (
	"database/sql"
	"encoding/json"
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
	KeyColumns     []string `json:"keyColumns"`
	PageSize       int      `json:"pageSize"`
	DefaultSortCol string   `json:"defaultSortCol"`
	DefaultSortDir string   `json:"defaultSortDir"`
}

// ValidationRule is one optional per-column rule enforced on every write.
type ValidationRule struct {
	Type  string `json:"type"`            // email | min_len | max_len | number | text
	Param int    `json:"param,omitempty"` // bound for min_len / max_len
}

type ColumnDef struct {
	ID               int64            `json:"id"`
	TableDefID       int64            `json:"tableDefId"`
	Name             string           `json:"name"`
	Label            string           `json:"label"`
	FieldType        string           `json:"fieldType"`
	EnumOptions      []string         `json:"enumOptions"`
	Editable         bool             `json:"editable"`
	Required         bool             `json:"required"`
	Visible          bool             `json:"visible"`
	Searchable       bool             `json:"searchable"`
	Sortable         bool             `json:"sortable"`
	Position         int              `json:"position"`
	Validations      []ValidationRule `json:"validations,omitempty"`
	BaseType         string           `json:"baseType,omitempty"`
	FKTableDefID     int64            `json:"fkTableDefId,omitempty"`
	FKRefColumn      string           `json:"fkRefColumn,omitempty"`
	FKDisplayColumns []string         `json:"fkDisplayColumns,omitempty"`
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
			m2m_junction_def_id,m2m_junction_src_col,m2m_junction_tgt_col,m2m_display_cols,validations)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			defID, c.Name, c.Label, c.FieldType, opts,
			c.Editable, c.Required, c.Visible, c.Searchable, c.Sortable, c.Position,
			c.BaseType, c.FKTableDefID, c.FKRefColumn, disp,
			c.M2MJunctionDefID, c.M2MJunctionSrcCol, c.M2MJunctionTgtCol, m2mDisp, vRules)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveTableDef(def *TableDef, cols []ColumnDef) error {
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
	res, err := tx.Exec(`INSERT INTO table_defs(datasource_id,schema_name,table_name,label,key_columns,page_size,default_sort_col,default_sort_dir)
		VALUES(?,?,?,?,?,?,?,?)`, def.DatasourceID, def.SchemaName, def.TableName, def.Label, string(kj), def.PageSize, def.DefaultSortCol, def.DefaultSortDir)
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
	res, err := tx.Exec(`UPDATE table_defs SET datasource_id=?,schema_name=?,table_name=?,label=?,key_columns=?,page_size=?,default_sort_col=?,default_sort_dir=?
		WHERE id=?`, def.DatasourceID, def.SchemaName, def.TableName, def.Label, string(kj), def.PageSize, def.DefaultSortCol, def.DefaultSortDir, def.ID)
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
	rows, err := s.db.Query(`SELECT id,datasource_id,schema_name,table_name,label,key_columns,page_size,default_sort_col,default_sort_dir
		FROM table_defs ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TableDef
	for rows.Next() {
		var d TableDef
		var kj string
		if err := rows.Scan(&d.ID, &d.DatasourceID, &d.SchemaName, &d.TableName, &d.Label, &kj, &d.PageSize, &d.DefaultSortCol, &d.DefaultSortDir); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(kj), &d.KeyColumns)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetTableDef(id int64) (*TableDef, []ColumnDef, error) {
	d := &TableDef{}
	var kj string
	err := s.db.QueryRow(`SELECT id,datasource_id,schema_name,table_name,label,key_columns,page_size,default_sort_col,default_sort_dir
		FROM table_defs WHERE id=?`, id).
		Scan(&d.ID, &d.DatasourceID, &d.SchemaName, &d.TableName, &d.Label, &kj, &d.PageSize, &d.DefaultSortCol, &d.DefaultSortDir)
	if err == sql.ErrNoRows {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	json.Unmarshal([]byte(kj), &d.KeyColumns)
	cols, err := s.getColumns(id)
	return d, cols, err
}

func (s *Store) getColumns(defID int64) ([]ColumnDef, error) {
	rows, err := s.db.Query(`SELECT id,table_def_id,name,label,field_type,enum_options,
		editable,required,visible,searchable,sortable,position,
		base_type,fk_table_def_id,fk_ref_column,fk_display_columns,
		m2m_junction_def_id,m2m_junction_src_col,m2m_junction_tgt_col,m2m_display_cols,validations
		FROM columns WHERE table_def_id=? ORDER BY position`, defID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ColumnDef
	for rows.Next() {
		var c ColumnDef
		var opts, disp, m2mDisp, vld sql.NullString
		if err := rows.Scan(&c.ID, &c.TableDefID, &c.Name, &c.Label, &c.FieldType, &opts,
			&c.Editable, &c.Required, &c.Visible, &c.Searchable, &c.Sortable, &c.Position,
			&c.BaseType, &c.FKTableDefID, &c.FKRefColumn, &disp,
			&c.M2MJunctionDefID, &c.M2MJunctionSrcCol, &c.M2MJunctionTgtCol, &m2mDisp, &vld); err != nil {
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
