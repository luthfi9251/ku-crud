package meta

import (
	"database/sql"
	"encoding/json"
)

type TableDef struct {
	ID           int64  `json:"id"`
	DatasourceID int64  `json:"datasourceId"`
	SchemaName   string `json:"schemaName"`
	TableName    string `json:"tableName"`
	Label        string `json:"label"`
	PKColumn     string `json:"pkColumn"`
	PageSize     int    `json:"pageSize"`
}

type ColumnDef struct {
	ID          int64    `json:"id"`
	TableDefID  int64    `json:"tableDefId"`
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

func insertCols(tx *sql.Tx, defID int64, cols []ColumnDef) error {
	for _, c := range cols {
		var opts any
		if len(c.EnumOptions) > 0 {
			b, _ := json.Marshal(c.EnumOptions)
			opts = string(b)
		}
		_, err := tx.Exec(`INSERT INTO columns(table_def_id,name,label,field_type,enum_options,
			editable,required,visible,searchable,sortable,position)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			defID, c.Name, c.Label, c.FieldType, opts,
			c.Editable, c.Required, c.Visible, c.Searchable, c.Sortable, c.Position)
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
	res, err := tx.Exec(`INSERT INTO table_defs(datasource_id,schema_name,table_name,label,pk_column,page_size)
		VALUES(?,?,?,?,?,?)`, def.DatasourceID, def.SchemaName, def.TableName, def.Label, def.PKColumn, def.PageSize)
	if err != nil {
		tx.Rollback()
		return err
	}
	def.ID, _ = res.LastInsertId()
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
	res, err := tx.Exec(`UPDATE table_defs SET datasource_id=?,schema_name=?,table_name=?,label=?,pk_column=?,page_size=?
		WHERE id=?`, def.DatasourceID, def.SchemaName, def.TableName, def.Label, def.PKColumn, def.PageSize, def.ID)
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
	rows, err := s.db.Query(`SELECT id,datasource_id,schema_name,table_name,label,pk_column,page_size
		FROM table_defs ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TableDef
	for rows.Next() {
		var d TableDef
		if err := rows.Scan(&d.ID, &d.DatasourceID, &d.SchemaName, &d.TableName, &d.Label, &d.PKColumn, &d.PageSize); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetTableDef(id int64) (*TableDef, []ColumnDef, error) {
	d := &TableDef{}
	err := s.db.QueryRow(`SELECT id,datasource_id,schema_name,table_name,label,pk_column,page_size
		FROM table_defs WHERE id=?`, id).
		Scan(&d.ID, &d.DatasourceID, &d.SchemaName, &d.TableName, &d.Label, &d.PKColumn, &d.PageSize)
	if err == sql.ErrNoRows {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	cols, err := s.getColumns(id)
	return d, cols, err
}

func (s *Store) getColumns(defID int64) ([]ColumnDef, error) {
	rows, err := s.db.Query(`SELECT id,table_def_id,name,label,field_type,enum_options,
		editable,required,visible,searchable,sortable,position
		FROM columns WHERE table_def_id=? ORDER BY position`, defID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ColumnDef
	for rows.Next() {
		var c ColumnDef
		var opts sql.NullString
		if err := rows.Scan(&c.ID, &c.TableDefID, &c.Name, &c.Label, &c.FieldType, &opts,
			&c.Editable, &c.Required, &c.Visible, &c.Searchable, &c.Sortable, &c.Position); err != nil {
			return nil, err
		}
		if opts.Valid && opts.String != "" {
			json.Unmarshal([]byte(opts.String), &c.EnumOptions)
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
