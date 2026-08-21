package api

import (
	"fmt"
	"net/http"
	"time"

	"ku-crud/internal/meta"
)

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
}

type metaFileTable struct {
	DatasourceRef  string           `json:"datasourceRef"`
	Schema         string           `json:"schema"`
	Table          string           `json:"table"`
	Label          string           `json:"label"`
	KeyColumns     []string         `json:"keyColumns"`
	PageSize       int              `json:"pageSize"`
	DefaultSortCol string           `json:"defaultSortCol"`
	DefaultSortDir string           `json:"defaultSortDir"`
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
			Table: d.TableName, Label: d.Label, KeyColumns: nonNil(d.KeyColumns), PageSize: d.PageSize,
			DefaultSortCol: d.DefaultSortCol, DefaultSortDir: d.DefaultSortDir,
			GroupRef: groupName[d.GroupID], Columns: []metaFileColumn{}}
		for _, c := range cols {
			fc := metaFileColumn{Name: c.Name, Label: c.Label, FieldType: c.FieldType,
				EnumOptions: nonNil(c.EnumOptions), Editable: c.Editable, Required: c.Required,
				Visible: c.Visible, Searchable: c.Searchable, Sortable: c.Sortable,
				Position: c.Position, BaseType: c.BaseType, Validations: nonNilRules(c.Validations),
				FKRefColumn: c.FKRefColumn, FKDisplay: nonNil(c.FKDisplayColumns),
				M2MSrcCol: c.M2MJunctionSrcCol, M2MTgtCol: c.M2MJunctionTgtCol, M2MDisplay: nonNil(c.M2MDisplayColumns)}
			if c.FKTableDefID > 0 {
				fc.FKTableRef = refOf(c.FKTableDefID)
			}
			if c.M2MJunctionDefID > 0 {
				fc.M2MJunction = refOf(c.M2MJunctionDefID)
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
