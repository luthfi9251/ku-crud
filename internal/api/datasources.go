package api

import (
	"errors"
	"net/http"

	"ku-crud/internal/ds"
	"ku-crud/internal/meta"
)

type dsDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"dbname"`
	Username string `json:"username"`
	SSLMode  string `json:"sslmode"`
}

// dsInput accepts password on input; meta.Datasource's json:"-" would drop it.
type dsInput struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"dbname"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"sslmode"`
}

func (in dsInput) toDS() meta.Datasource {
	return meta.Datasource{Name: in.Name, Host: in.Host, Port: in.Port,
		DBName: in.DBName, Username: in.Username, Password: in.Password, SSLMode: in.SSLMode}
}

func (s *Server) toDTO(d meta.Datasource) dsDTO {
	return dsDTO{s.ids.Encode("ds", d.ID), d.Name, d.Host, d.Port, d.DBName, d.Username, d.SSLMode}
}

// dsCtx decodes the {id} path token into a datasource id. Bad or unknown
// tokens surface as ErrNotFound (404) — never a raw id.
func (s *Server) dsCtx(r *http.Request) (int64, error) {
	id, err := s.ids.Decode("ds", r.PathValue("id"))
	if err != nil {
		return 0, meta.ErrNotFound
	}
	if _, err := s.store.GetDatasource(id); err != nil {
		return 0, err
	}
	return id, nil
}

func validDS(d *meta.Datasource) string {
	if d.Name == "" || d.Host == "" || d.DBName == "" || d.Username == "" {
		return "name, host, dbname, username are required"
	}
	if d.Port <= 0 {
		return "port must be positive"
	}
	return ""
}

// dsToDSN assembles the live-connection DSN for a stored datasource.
func dsToDSN(d *meta.Datasource) ds.DSN {
	return ds.DSN{Host: d.Host, Port: d.Port, DBName: d.DBName,
		Username: d.Username, Password: d.Password, SSLMode: d.SSLMode, Raw: d.Raw}
}

func (s *Server) handleDSCreate(w http.ResponseWriter, r *http.Request) {
	var in dsInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	d := in.toDS()
	if msg := validDS(&d); msg != "" {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	if err := s.store.CreateDatasource(&d); err != nil {
		writeErr(w, 400, "VALIDATION", "could not create datasource (duplicate name?)", err.Error())
		return
	}
	writeJSON(w, 200, s.toDTO(d))
}

func (s *Server) handleDSList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListDatasources()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	out := make([]dsDTO, len(list))
	for i, d := range list {
		out[i] = s.toDTO(d)
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleDSUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := s.dsCtx(r)
	if err != nil {
		s.writeDSErr(w, err)
		return
	}
	old, err := s.store.GetDatasource(id)
	if errors.Is(err, meta.ErrNotFound) {
		writeErr(w, 404, "NOT_FOUND", "datasource not found", nil)
		return
	}
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	var in dsInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	d := in.toDS()
	if d.Password == "" {
		d.Password = old.Password
	}
	d.Raw = old.Raw // conn-string passthrough is never submitted via API
	if msg := validDS(&d); msg != "" {
		writeErr(w, 400, "VALIDATION", msg, nil)
		return
	}
	d.ID = id
	if err := s.store.UpdateDatasource(&d); err != nil {
		writeErr(w, 400, "VALIDATION", "update failed", err.Error())
		return
	}
	writeJSON(w, 200, s.toDTO(d))
}

func (s *Server) handleDSDelete(w http.ResponseWriter, r *http.Request) {
	id, err := s.dsCtx(r)
	if err != nil {
		s.writeDSErr(w, err)
		return
	}
	if err := s.store.DeleteDatasource(id); err != nil {
		if errors.Is(err, meta.ErrNotFound) {
			writeErr(w, 404, "NOT_FOUND", "datasource not found", nil)
			return
		}
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) writeDSErr(w http.ResponseWriter, err error) {
	if errors.Is(err, meta.ErrNotFound) {
		writeErr(w, 404, "NOT_FOUND", "datasource not found", nil)
		return
	}
	writeErr(w, 500, "INTERNAL", "server error", nil)
}

func (s *Server) handleDSTest(w http.ResponseWriter, r *http.Request) {
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
	db, err := ds.Connect(dsToDSN(d))
	if err != nil {
		writeErr(w, 502, "CONN", "connection failed", err.Error())
		return
	}
	defer db.Close()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
