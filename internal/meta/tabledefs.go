package meta

import (
	"database/sql"
	"encoding/json"
)

// SelfRef marks an FK target as "this table definition"; Save/Update rewrite
// it to the real def id before columns hit the store.
const SelfRef int64 = -1

type TableDef struct {
	ID           int64    `json:"id"`
	DatasourceID int64    `json:"datasourceId"`
	SchemaName   string   `json:"schemaName"`
	TableName    string   `json:"tableName"`
	Label        string   `json:"label"`
	KeyColumns   []string `json:"keyColumns"`
	PageSize     int      `json:"pageSize"`
}

type ColumnDef struct {
	ID               int64    `json:"id"`
	TableDefID       int64    `json:"tableDefId"`
	Name             string   `json:"name"`
	Label            string   `json:"label"`
	FieldType        string   `json:"fieldType"`
	EnumOptions      []string `json:"enumOptions"`
	Editable         bool     `json:"editable"`
	Required         bool     `json:"required"`
	Visible          bool     `json:"visible"`
	Searchable       bool     `json:"searchable"`
	Sortable         bool     `json:"sortable"`
	Position         int      `json:"position"`
	BaseType         string   `json:"baseType,omitempty"`
	FKTableDefID     int64    `json:"fkTableDefId,omitempty"`
	FKRefColumn      string   `json:"fkRefColumn,omitempty"`
	FKDisplayColumns []string `json:"fkDisplayColumns,omitempty"`
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
		_, err := tx.Exec(`INSERT INTO columns(table_def_id,name,label,field_type,enum_options,
			editable,required,visible,searchable,sortable,position,
			base_type,fk_table_def_id,fk_ref_column,fk_display_columns)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			defID, c.Name, c.Label, c.FieldType, opts,
			c.Editable, c.Required, c.Visible, c.Searchable, c.Sortable, c.Position,
			c.BaseType, c.FKTableDefID, c.FKRefColumn, disp)
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
	res, err := tx.Exec(`INSERT INTO table_defs(datasource_id,schema_name,table_name,label,key_columns,page_size)
		VALUES(?,?,?,?,?,?)`, def.DatasourceID, def.SchemaName, def.TableName, def.Label, string(kj), def.PageSize)
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
	res, err := tx.Exec(`UPDATE table_defs SET datasource_id=?,schema_name=?,table_name=?,label=?,key_columns=?,page_size=?
		WHERE id=?`, def.DatasourceID, def.SchemaName, def.TableName, def.Label, string(kj), def.PageSize, def.ID)
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
	rows, err := s.db.Query(`SELECT id,datasource_id,schema_name,table_name,label,key_columns,page_size
		FROM table_defs ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TableDef
	for rows.Next() {
		var d TableDef
		var kj string
		if err := rows.Scan(&d.ID, &d.DatasourceID, &d.SchemaName, &d.TableName, &d.Label, &kj, &d.PageSize); err != nil {
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
	err := s.db.QueryRow(`SELECT id,datasource_id,schema_name,table_name,label,key_columns,page_size
		FROM table_defs WHERE id=?`, id).
		Scan(&d.ID, &d.DatasourceID, &d.SchemaName, &d.TableName, &d.Label, &kj, &d.PageSize)
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
		base_type,fk_table_def_id,fk_ref_column,fk_display_columns
		FROM columns WHERE table_def_id=? ORDER BY position`, defID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ColumnDef
	for rows.Next() {
		var c ColumnDef
		var opts, disp sql.NullString
		if err := rows.Scan(&c.ID, &c.TableDefID, &c.Name, &c.Label, &c.FieldType, &opts,
			&c.Editable, &c.Required, &c.Visible, &c.Searchable, &c.Sortable, &c.Position,
			&c.BaseType, &c.FKTableDefID, &c.FKRefColumn, &disp); err != nil {
			return nil, err
		}
		if opts.Valid && opts.String != "" {
			json.Unmarshal([]byte(opts.String), &c.EnumOptions)
		}
		if disp.Valid && disp.String != "" {
			json.Unmarshal([]byte(disp.String), &c.FKDisplayColumns)
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
