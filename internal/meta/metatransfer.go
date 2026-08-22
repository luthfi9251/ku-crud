package meta

import (
	"encoding/json"
	"fmt"
)

// DefRef is a natural-key reference to a table definition.
type DefRef struct{ DsName, Schema, Table string }

type PlannedColumn struct {
	Col     ColumnDef
	FKRef   DefRef // zero DsName -> Col.FKTableDefID already local/self
	M2MJRef DefRef
}

type PlannedDef struct {
	LocalID   int64 // >0 = overwrite existing def
	Def       TableDef
	DsName    string
	GroupName string // "" = ungrouped
	Columns   []PlannedColumn
}

// PlannedDatasource resolves BY NAME (LocalID is an optional hint, not the
// lookup key): name found locally -> overwrite (Password replaced only when
// non-empty, otherwise the stored password is kept); name not found -> create.
type PlannedDatasource struct {
	LocalID  int64
	DS       Datasource
	Password string // required for create; empty on overwrite keeps existing
}

type ImportPlan struct {
	Groups      []string // create if name missing
	Datasources []PlannedDatasource
	Defs        []PlannedDef
}

// ApplyImport executes the whole plan in ONE SQLite transaction: groups first,
// then datasources (by name), then def rows (pass 1, to allocate ids), then
// columns (pass 2, fk/junction refs resolved via natural keys against local
// defs + imported defs + self). Any hard failure rolls everything back.
//
// The store runs with MaxOpenConns(1), so every query issued on s.db while a
// transaction is open would block forever on the single connection — hence
// passwords are encrypted BEFORE the transaction starts, and everything inside
// runs on tx only.
func (s *Store) ApplyImport(p ImportPlan) (createdDefs, updatedDefs int, err error) {
	// pre-encrypt passwords outside the transaction (see note above); the
	// create path always uses enc[i], the overwrite path only when non-empty.
	enc := make([]string, len(p.Datasources))
	for i := range p.Datasources {
		e, e2 := encryptPassword(s, p.Datasources[i].Password)
		if e2 != nil {
			return 0, 0, e2
		}
		enc[i] = e
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	// groups (create-if-missing)
	groupID := map[string]int64{}
	for _, name := range p.Groups {
		var gid int64
		if e := tx.QueryRow(`SELECT id FROM table_groups WHERE name=?`, name).Scan(&gid); e != nil {
			res, e2 := tx.Exec(`INSERT INTO table_groups(name,position)
				VALUES(?, (SELECT COALESCE(MAX(position),-1)+1 FROM table_groups))`, name)
			if e2 != nil {
				return 0, 0, e2
			}
			gid, _ = res.LastInsertId()
		}
		groupID[name] = gid
	}

	// datasource ids by name: seeded from ALL local datasources so defs may
	// reference a local ds that is not part of the plan, then the planned ones
	// (create/update) override.
	dsID := map[string]int64{}
	{
		rows, e := tx.Query(`SELECT id,name FROM datasources`)
		if e != nil {
			return 0, 0, e
		}
		for rows.Next() {
			var id int64
			var name string
			if e := rows.Scan(&id, &name); e != nil {
				rows.Close()
				return 0, 0, e
			}
			dsID[name] = id
		}
		rows.Close()
		if e := rows.Err(); e != nil {
			return 0, 0, e
		}
	}
	for i, pd := range p.Datasources {
		var id int64
		if e := tx.QueryRow(`SELECT id FROM datasources WHERE name=?`, pd.DS.Name).Scan(&id); e == nil {
			var pw string
			if e2 := tx.QueryRow(`SELECT password FROM datasources WHERE id=?`, id).Scan(&pw); e2 != nil {
				return 0, 0, e2
			}
			newPw := pw
			if pd.Password != "" {
				newPw = enc[i]
			}
			if _, e2 := tx.Exec(`UPDATE datasources SET host=?,port=?,dbname=?,username=?,password=?,sslmode=?,driver=?,raw='' WHERE id=?`,
				pd.DS.Host, pd.DS.Port, pd.DS.DBName, pd.DS.Username, newPw, pd.DS.SSLMode, pd.DS.Driver, id); e2 != nil {
				return 0, 0, e2
			}
		} else {
			res, e2 := tx.Exec(`INSERT INTO datasources(name,host,port,dbname,username,password,sslmode,driver,raw) VALUES(?,?,?,?,?,?,?,?,?)`,
				pd.DS.Name, pd.DS.Host, pd.DS.Port, pd.DS.DBName, pd.DS.Username, enc[i], pd.DS.SSLMode, pd.DS.Driver, "")
			if e2 != nil {
				return 0, 0, e2
			}
			id, _ = res.LastInsertId()
		}
		dsID[pd.DS.Name] = id
	}

	// local def index for natural-key resolution
	type nk struct{ ds, schema, table string }
	refID := map[nk]int64{}
	{
		rows, e := tx.Query(`SELECT d.id, ds.name, d.schema_name, d.table_name FROM table_defs d JOIN datasources ds ON ds.id=d.datasource_id`)
		if e != nil {
			return 0, 0, e
		}
		for rows.Next() {
			var id int64
			var dsName, sch, tbl string
			if e := rows.Scan(&id, &dsName, &sch, &tbl); e != nil {
				rows.Close()
				return 0, 0, e
			}
			refID[nk{dsName, sch, tbl}] = id
		}
		rows.Close()
		if e := rows.Err(); e != nil {
			return 0, 0, e
		}
	}

	// pass 1: def rows
	keyOf := func(d PlannedDef) nk { return nk{d.DsName, d.Def.SchemaName, d.Def.TableName} }
	pending := map[nk]int64{}
	for i := range p.Defs {
		d := &p.Defs[i]
		d.Def.DatasourceID = dsID[d.DsName]
		var gid any
		if g, ok := groupID[d.GroupName]; ok && d.GroupName != "" {
			gid = g
		}
		kj, e := json.Marshal(d.Def.KeyColumns)
		if e != nil {
			return 0, 0, e
		}
		if d.LocalID > 0 {
			if _, e := tx.Exec(`UPDATE table_defs SET datasource_id=?,schema_name=?,table_name=?,label=?,key_columns=?,page_size=?,default_sort_col=?,default_sort_dir=?,default_view=?,view_config=?,group_id=?,hooks=? WHERE id=?`,
				d.Def.DatasourceID, d.Def.SchemaName, d.Def.TableName, d.Def.Label, string(kj), d.Def.PageSize,
				d.Def.DefaultSortCol, d.Def.DefaultSortDir, d.Def.DefaultView, d.Def.ViewConfig, gid, d.Def.Hooks, d.LocalID); e != nil {
				return 0, 0, e
			}
			d.Def.ID = d.LocalID
			updatedDefs++
		} else {
			res, e := tx.Exec(`INSERT INTO table_defs(datasource_id,schema_name,table_name,label,key_columns,page_size,default_sort_col,default_sort_dir,default_view,view_config,group_id,hooks) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
				d.Def.DatasourceID, d.Def.SchemaName, d.Def.TableName, d.Def.Label, string(kj), d.Def.PageSize,
				d.Def.DefaultSortCol, d.Def.DefaultSortDir, d.Def.DefaultView, d.Def.ViewConfig, gid, d.Def.Hooks)
			if e != nil {
				return 0, 0, e
			}
			d.Def.ID, _ = res.LastInsertId()
			createdDefs++
		}
		pending[keyOf(p.Defs[i])] = d.Def.ID
	}
	for k, v := range pending {
		refID[k] = v
	}

	// pass 2: columns with resolved refs (ALWAYS re-wired, even overwrite-
	// identical bundles — subset-level equality must not skip remapping)
	for i := range p.Defs {
		d := &p.Defs[i]
		if _, e := tx.Exec(`DELETE FROM columns WHERE table_def_id=?`, d.Def.ID); e != nil {
			return 0, 0, e
		}
		cols := make([]ColumnDef, len(d.Columns))
		for j, pc := range d.Columns {
			c := pc.Col
			c.TableDefID = d.Def.ID
			if pc.FKRef.DsName != "" {
				k := nk{pc.FKRef.DsName, pc.FKRef.Schema, pc.FKRef.Table}
				if k == keyOf(*d) {
					c.FKTableDefID = SelfRef
				} else if id, ok := refID[k]; ok {
					c.FKTableDefID = id
				} else {
					return 0, 0, fmt.Errorf("table %s: column %s references unknown table %s/%s/%s",
						d.Def.TableName, c.Name, pc.FKRef.DsName, pc.FKRef.Schema, pc.FKRef.Table)
				}
			}
			if pc.M2MJRef.DsName != "" {
				k := nk{pc.M2MJRef.DsName, pc.M2MJRef.Schema, pc.M2MJRef.Table}
				if id, ok := refID[k]; ok && k != keyOf(*d) {
					c.M2MJunctionDefID = id
				} else {
					return 0, 0, fmt.Errorf("table %s: column %s junction %s/%s/%s not resolvable",
						d.Def.TableName, c.Name, pc.M2MJRef.DsName, pc.M2MJRef.Schema, pc.M2MJRef.Table)
				}
			}
			cols[j] = c
		}
		resolveSelfRefs(d.Def.ID, cols)
		if e := insertCols(tx, d.Def.ID, cols); e != nil {
			return 0, 0, e
		}
	}
	return createdDefs, updatedDefs, tx.Commit()
}
