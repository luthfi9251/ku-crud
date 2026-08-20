package meta

import "database/sql"

type Datasource struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"dbname"`
	Username string `json:"username"`
	Password string `json:"-"`
	SSLMode  string `json:"sslmode"`
	Driver   string `json:"driver"`
	Raw      string `json:"-"` // conn-string passthrough (tests)
}

func (s *Store) CreateDatasource(d *Datasource) error {
	res, err := s.db.Exec(`INSERT INTO datasources(name,host,port,dbname,username,password,sslmode,driver,raw)
		VALUES(?,?,?,?,?,?,?,?,?)`, d.Name, d.Host, d.Port, d.DBName, d.Username, d.Password, d.SSLMode, d.Driver, d.Raw)
	if err != nil {
		return err
	}
	d.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) GetDatasource(id int64) (*Datasource, error) {
	d := &Datasource{}
	err := s.db.QueryRow(`SELECT id,name,host,port,dbname,username,password,sslmode,driver,raw
		FROM datasources WHERE id=?`, id).
		Scan(&d.ID, &d.Name, &d.Host, &d.Port, &d.DBName, &d.Username, &d.Password, &d.SSLMode, &d.Driver, &d.Raw)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *Store) ListDatasources() ([]Datasource, error) {
	rows, err := s.db.Query(`SELECT id,name,host,port,dbname,username,password,sslmode,driver,raw
		FROM datasources ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Datasource
	for rows.Next() {
		var d Datasource
		if err := rows.Scan(&d.ID, &d.Name, &d.Host, &d.Port, &d.DBName, &d.Username, &d.Password, &d.SSLMode, &d.Driver, &d.Raw); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) UpdateDatasource(d *Datasource) error {
	res, err := s.db.Exec(`UPDATE datasources SET name=?,host=?,port=?,dbname=?,username=?,password=?,sslmode=?,driver=?,raw=?
		WHERE id=?`, d.Name, d.Host, d.Port, d.DBName, d.Username, d.Password, d.SSLMode, d.Driver, d.Raw, d.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteDatasource(id int64) error {
	res, err := s.db.Exec(`DELETE FROM datasources WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
