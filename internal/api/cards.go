package api

import (
	"net/http"

	"ku-crud/internal/meta"
)

type statCardDTO struct {
	ID         string `json:"id"`
	TableDefID string `json:"tableDefId"`
	TableName  string `json:"tableName"` // data address; empty for query views (use tableDefId token)
	TableLabel string `json:"tableLabel"`
	Label      string `json:"label"`
	Func       string `json:"func"`
	Column     string `json:"column"`
	Filters    string `json:"filters"`
	Position   int    `json:"position"`
}

// validateCard re-checks func/column/type rules against the CURRENT def
// (defs are runtime-mutable). Mirrors engine's Stats rules; serve-time
// validation remains the enforcement point.
func (s *Server) validateCard(def *meta.TableDef, cols []meta.ColumnDef, fn, column string) string {
	switch fn {
	case "count":
		if column != "" {
			return "count takes no column"
		}
	case "sum", "avg", "min", "max":
		var col *meta.ColumnDef
		for i := range cols {
			if cols[i].Name == column {
				col = &cols[i]
				break
			}
		}
		if col == nil || col.FieldType == "m2m" || col.IsComputed {
			return "unknown or virtual column " + column
		}
		if fn == "sum" || fn == "avg" {
			if col.FieldType != "number" {
				return fn + " requires a number column"
			}
		} else if col.FieldType != "number" && col.FieldType != "datetime" {
			return fn + " requires a number or datetime column"
		}
	default:
		return "func must be one of count|sum|avg|min|max"
	}
	return ""
}

func (s *Server) cardDTO(c meta.StatCard, def *meta.TableDef) statCardDTO {
	return statCardDTO{
		ID: s.ids.Encode("card", c.ID), TableDefID: s.ids.Encode("td", c.TableDefID),
		TableName: def.TableName, TableLabel: def.Label,
		Label: c.Label, Func: c.Func, Column: c.Column, Filters: c.Filters, Position: c.Position,
	}
}

func (s *Server) handleCardList(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	cards, err := s.store.ListStatCards()
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	out := []statCardDTO{}
	for _, c := range cards {
		def, _, err := s.store.GetTableDef(c.TableDefID)
		if err != nil {
			continue // def vanished between list and read (cascade races)
		}
		if !s.hasTablePerm(u, def.ID, "read") {
			continue
		}
		out = append(out, s.cardDTO(c, def))
	}
	writeJSON(w, 200, out)
}

func (s *Server) cardCtx(r *http.Request) (*meta.StatCard, error) {
	id, err := s.ids.Decode("card", r.PathValue("id"))
	if err != nil {
		return nil, meta.ErrNotFound
	}
	return s.store.GetStatCard(id)
}

type cardInput struct {
	TableDefID string `json:"tableDefId"`
	Label      string `json:"label"`
	Func       string `json:"func"`
	Column     string `json:"column"`
	Filters    string `json:"filters"`
}

func (s *Server) readCardInput(w http.ResponseWriter, r *http.Request, in *cardInput) (*meta.TableDef, bool) {
	if err := readJSON(r, in); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return nil, false
	}
	if len(in.Label) < 1 || len(in.Label) > 60 {
		writeErr(w, 400, "VALIDATION", "card label must be 1..60 chars", nil)
		return nil, false
	}
	tdID, err := s.ids.Decode("td", in.TableDefID)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "unknown table", nil)
		return nil, false
	}
	def, cols, err := s.store.GetTableDef(tdID)
	if err != nil {
		writeErr(w, 400, "VALIDATION", "unknown table", nil)
		return nil, false
	}
	if msg := s.validateCard(def, cols, in.Func, in.Column); msg != "" {
		writeErr(w, 400, "STATS_INVALID", msg, nil)
		return nil, false
	}
	if in.Filters == "" {
		in.Filters = "[]"
	}
	u := userFrom(r)
	if _, fmsg := s.parseFilters(def, cols, u, in.Filters); fmsg != "" {
		writeErr(w, 400, "FILTER_INVALID", fmsg, nil)
		return nil, false
	}
	return def, true
}

func (s *Server) handleCardCreate(w http.ResponseWriter, r *http.Request) {
	var in cardInput
	def, ok := s.readCardInput(w, r, &in)
	if !ok {
		return
	}
	id, err := s.store.CreateStatCard(def.ID, in.Label, in.Func, in.Column, in.Filters)
	if err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	card, _ := s.store.GetStatCard(id)
	writeJSON(w, 200, s.cardDTO(*card, def))
}

func (s *Server) handleCardUpdate(w http.ResponseWriter, r *http.Request) {
	card, err := s.cardCtx(r)
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "card not found", nil)
		return
	}
	var in cardInput
	def, ok := s.readCardInput(w, r, &in)
	if !ok {
		return
	}
	if err := s.store.UpdateStatCard(card.ID, in.Label, in.Func, in.Column, in.Filters); err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	updated, _ := s.store.GetStatCard(card.ID)
	writeJSON(w, 200, s.cardDTO(*updated, def))
}

func (s *Server) handleCardDelete(w http.ResponseWriter, r *http.Request) {
	card, err := s.cardCtx(r)
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "card not found", nil)
		return
	}
	if err := s.store.DeleteStatCard(card.ID); err != nil {
		writeErr(w, 500, "INTERNAL", "server error", nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleCardMove(w http.ResponseWriter, r *http.Request) {
	card, err := s.cardCtx(r)
	if err != nil {
		writeErr(w, 404, "NOT_FOUND", "card not found", nil)
		return
	}
	var in struct {
		Dir string `json:"dir"`
	}
	if err := readJSON(r, &in); err != nil || (in.Dir != "up" && in.Dir != "down") {
		writeErr(w, 400, "VALIDATION", "dir must be up or down", nil)
		return
	}
	if err := s.store.MoveStatCard(card.ID, in.Dir == "up"); err != nil {
		writeErr(w, 400, "VALIDATION", err.Error(), nil)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
