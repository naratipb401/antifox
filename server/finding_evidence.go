package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif" // register the gif decoder
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"antifox/report"
)

// Finding evidence screenshot upload/listing/management. Files live under
// dataDir/report_assets/evidence/<node_id>/ (backed up with report_assets);
// the findings page loads images via /api/findings/{id}/evidence/{file} (cookie
// auth, same as the logo); reports/MD embed them by disk path.
//
// Security baseline (this is a security platform; the upload endpoint must not
// become a malicious-file entry point):
//   - never trust the extension/client MIME: sniff the type from the content +
//     verify it decodes as a real image + re-encode (stripping appended/embedded
//     payloads, EXIF, polyglot tricks), always writing a clean png/jpg;
//   - decompression-bomb guard: check size limits via DecodeConfig before
//     decoding the full image;
//   - served responses force X-Content-Type-Options: nosniff + a fixed image
//     Content-Type + inline, preventing stored XSS.

const (
	evidenceMaxBytes  = 16 << 20   // 16MB per image
	evidenceMaxPixels = 50_000_000 // 50M pixel cap (decompression-bomb guard)
	evidenceMaxDim    = 20000
)

// sanitizeImage validates the image by content and re-encodes it; only
// png/jpeg/gif are accepted, always written as a clean png (or jpg).
func sanitizeImage(raw []byte) (out []byte, ext string, err error) {
	ct := http.DetectContentType(raw)
	switch ct {
	case "image/png", "image/jpeg", "image/gif":
	default:
		return nil, "", fmt.Errorf("only png/jpg/gif images are supported (got %s)", ct)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("not a valid image")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > evidenceMaxDim || cfg.Height > evidenceMaxDim ||
		cfg.Width*cfg.Height > evidenceMaxPixels {
		return nil, "", fmt.Errorf("image dimensions out of range")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("image decode failed")
	}
	var buf bytes.Buffer
	if ct == "image/jpeg" {
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, "", fmt.Errorf("image processing failed")
		}
		return buf.Bytes(), ".jpg", nil
	}
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", fmt.Errorf("image processing failed")
	}
	return buf.Bytes(), ".png", nil
}

// evidenceDir returns the evidence directory for a finding (absolute path).
func (s *Server) evidenceDir(nodeID int64) string {
	return filepath.Join(s.dataDir, "report_assets", "evidence", strconv.FormatInt(nodeID, 10))
}

// evidenceURL builds the image URL served to the frontend.
func evidenceURL(nodeID int64, filename string) string {
	return fmt.Sprintf("/api/findings/%d/evidence/%s", nodeID, filename)
}

// POST /api/findings/{id}/evidence - multipart "file" + optional caption / step_index.
func (s *Server) uploadFindingEvidence(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	nodeID, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "invalid finding id")
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil { // 16 MB
		writeErr(w, 400, "multipart parse failed")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "missing file")
		return
	}
	defer file.Close()
	// read into memory (16MB cap), then content-level validation + re-encode;
	// never trust the extension/client MIME.
	raw, err := io.ReadAll(io.LimitReader(file, evidenceMaxBytes+1))
	if err != nil {
		writeErr(w, 400, "failed to read file")
		return
	}
	if len(raw) > evidenceMaxBytes {
		writeErr(w, 400, "image too large (16MB max)")
		return
	}
	clean, ext, err := sanitizeImage(raw)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	dir := s.evidenceDir(nodeID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, 500, "failed to create directory")
		return
	}
	name := fmt.Sprintf("ev-%d%s", time.Now().UnixNano(), ext)
	if err := os.WriteFile(filepath.Join(dir, name), clean, 0o644); err != nil {
		writeErr(w, 500, "failed to write file")
		return
	}
	caption := strings.TrimSpace(r.FormValue("caption"))
	stepIndex, _ := strconv.Atoi(r.FormValue("step_index"))
	if caption == "" {
		// auto caption: when empty, derive one from the agent-recorded
		// reproduction text for that step (editable, no extra LLM call).
		caption = s.deriveEvidenceCaption(nodeID, stepIndex)
	}
	ev, err := pg.CreateFindingEvidence(nodeID, name, caption, stepIndex)
	if err != nil {
		_ = os.Remove(filepath.Join(dir, name)) // roll back the written file
		writeErr(w, 500, "save failed: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"evidence": ev, "url": evidenceURL(nodeID, name)})
}

// deriveEvidenceCaption builds a default figure caption for an evidence shot from the
// agent-written reproduction step it belongs to (step N: what that step proved) --
// the caption is generated from reproduction text the agent already recorded, no
// extra LLM call, and stays human-editable. Returns "" for step 0 (overview) or
// when the finding has no such step.
func (s *Server) deriveEvidenceCaption(nodeID int64, stepIndex int) string {
	if stepIndex <= 0 || s.m == nil || s.m.pg == nil {
		return ""
	}
	f, err := s.m.pg.GetFindingByNodeID(nodeID)
	if err != nil || f == nil || len(f.Reproduction) == 0 {
		return ""
	}
	var rp report.Reproduction
	if json.Unmarshal(f.Reproduction, &rp) != nil || stepIndex > len(rp.Steps) {
		return ""
	}
	st := rp.Steps[stepIndex-1]
	text := strings.TrimSpace(st.Proves)
	if text == "" {
		text = strings.TrimSpace(st.Expected)
	}
	if text == "" {
		return ""
	}
	text = strings.SplitN(text, "\n", 2)[0] // single line, suits a caption under the thumbnail
	return clipUTF8(fmt.Sprintf("Step %d: %s", stepIndex, text), 120)
}

// GET /api/findings/{id}/evidence - list all evidence for a finding (with image URLs).
func (s *Server) listFindingEvidence(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	nodeID, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "invalid finding id")
		return
	}
	list, err := pg.ListFindingEvidence(nodeID)
	if err != nil {
		writeErr(w, 500, "failed to load evidence")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		out = append(out, map[string]any{"evidence": e, "url": evidenceURL(e.NodeID, e.Filename)})
	}
	writeJSON(w, 200, map[string]any{"items": out})
}

// PATCH /api/findings/{id}/evidence/{eid} - update caption / step / sort order.
func (s *Server) updateFindingEvidence(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	eid, ok := pathInt(r, "eid")
	if !ok {
		writeErr(w, 400, "invalid evidence id")
		return
	}
	var req struct {
		Caption   string `json:"caption"`
		StepIndex int    `json:"step_index"`
		SortOrder int    `json:"sort_order"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if err := pg.UpdateFindingEvidence(eid, req.Caption, req.StepIndex, req.SortOrder); err != nil {
		writeErr(w, 500, "save failed: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// DELETE /api/findings/{id}/evidence/{eid} - delete the DB row + file on disk.
func (s *Server) deleteFindingEvidence(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	eid, ok := pathInt(r, "eid")
	if !ok {
		writeErr(w, 400, "invalid evidence id")
		return
	}
	ev, err := pg.GetFindingEvidence(eid)
	if err != nil {
		writeErr(w, 404, "evidence not found")
		return
	}
	if err := pg.DeleteFindingEvidence(eid); err != nil {
		writeErr(w, 500, "delete failed: "+err.Error())
		return
	}
	_ = os.Remove(filepath.Join(s.evidenceDir(ev.NodeID), filepath.Base(ev.Filename)))
	writeJSON(w, 200, map[string]any{"deleted": eid})
}

// GET /api/findings/{id}/evidence/{file} - serve an evidence image (base name,
// traversal-safe; cookie auth like the logo).
func (s *Server) serveFindingEvidence(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "invalid finding id")
		return
	}
	file := filepath.Base(r.PathValue("file"))
	// fixed image Content-Type + nosniff + inline: even if the on-disk file is
	// tampered with, the browser never renders it as HTML/script.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch strings.ToLower(filepath.Ext(file)) {
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	default:
		writeErr(w, 404, "not found")
		return
	}
	w.Header().Set("Content-Disposition", "inline")
	http.ServeFile(w, r, filepath.Join(s.evidenceDir(nodeID), file))
}
