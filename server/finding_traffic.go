package server

import (
	"net/http"

	"antifox/traffic"
)

// finding↔traffic association API. Associations live in PG; packet metadata is resolved
// from the traffic store (SQLite) by id.
// Manual association goes through here (source=manual); the task engine can later call
// pg.LinkFindingTraffic(..., "auto") directly to record automatic links.

// POST /api/findings/{id}/traffic — body {exchange_ids:[]} links traffic to a finding.
func (s *Server) linkFindingTraffic(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	nodeID, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "invalid finding id")
		return
	}
	var req struct {
		ExchangeIDs []string `json:"exchange_ids"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "malformed request")
		return
	}
	if err := pg.LinkFindingTraffic(nodeID, req.ExchangeIDs, "manual"); err != nil {
		writeErr(w, 500, "link failed: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "linked": len(req.ExchangeIDs)})
}

// GET /api/findings/{id}/traffic — list traffic linked to the finding (with metadata; packet bodies fetched separately on demand).
func (s *Server) listFindingTraffic(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	nodeID, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "invalid finding id")
		return
	}
	ids, err := pg.ListFindingTrafficIDs(nodeID)
	if err != nil {
		writeErr(w, 500, "fetch failed")
		return
	}
	var metas []traffic.ExchangeMeta
	if tr := s.m.Traffic(); tr != nil && len(ids) > 0 {
		metas, _ = tr.MetaByIDs(ids)
	}
	writeJSON(w, 200, map[string]any{"exchanges": trafficDTOs(metas)})
}

// DELETE /api/findings/{id}/traffic/{eid} — remove the association.
func (s *Server) unlinkFindingTraffic(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	nodeID, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "invalid finding id")
		return
	}
	if err := pg.UnlinkFindingTraffic(nodeID, r.PathValue("eid")); err != nil {
		writeErr(w, 500, "unlink failed: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
