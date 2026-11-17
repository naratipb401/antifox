package db

import (
	"strconv"
	"strings"
	"time"
)

// Finding evidence screenshot data layer. File bodies live under dataDir/report_assets/evidence/<node_id>/; this table stores metadata only.
// Keyed by exploration node id, aligned with finding_enrichment; shared by the findings page / report / MD export.

type FindingEvidence struct {
	ID        int64     `json:"id"`
	NodeID    int64     `json:"node_id"`
	Filename  string    `json:"filename"`
	Caption   string    `json:"caption"`
	StepIndex int       `json:"step_index"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateFindingEvidence adds one evidence record (file already on disk); sort_order defaults to current max + 1.
func (d *DB) CreateFindingEvidence(nodeID int64, filename, caption string, stepIndex int) (*FindingEvidence, error) {
	var next int
	_ = d.QueryRow(`SELECT COALESCE(MAX(sort_order),0)+1 FROM finding_evidence WHERE node_id=$1`, nodeID).Scan(&next)
	var e FindingEvidence
	err := d.QueryRow(`
INSERT INTO finding_evidence(node_id, filename, caption, step_index, sort_order)
VALUES ($1,$2,$3,$4,$5)
RETURNING id, node_id, filename, caption, step_index, sort_order, created_at`,
		nodeID, filename, strings.TrimSpace(caption), stepIndex, next).
		Scan(&e.ID, &e.NodeID, &e.Filename, &e.Caption, &e.StepIndex, &e.SortOrder, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListFindingEvidence lists all evidence of a finding in display order.
func (d *DB) ListFindingEvidence(nodeID int64) ([]FindingEvidence, error) {
	rows, err := d.Query(`
SELECT id, node_id, filename, caption, step_index, sort_order, created_at
FROM finding_evidence WHERE node_id=$1 ORDER BY step_index, sort_order, id`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FindingEvidence
	for rows.Next() {
		var e FindingEvidence
		if err := rows.Scan(&e.ID, &e.NodeID, &e.Filename, &e.Caption, &e.StepIndex, &e.SortOrder, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountFindingEvidence counts each finding's evidence screenshots in batch (the findings list shows "N evidence items").
// node ids are int64 and inlined into IN directly — no injection risk.
func (d *DB) CountFindingEvidence(nodeIDs []int64) map[int64]int {
	out := map[int64]int{}
	if len(nodeIDs) == 0 {
		return out
	}
	// Fully parameterized ($1,$2,...): int64 node ids already rule out injection, yet placeholders are used to avoid any concatenated SQL.
	parts := make([]string, len(nodeIDs))
	args := make([]any, len(nodeIDs))
	for i, id := range nodeIDs {
		parts[i] = "$" + strconv.Itoa(i+1)
		args[i] = id
	}
	rows, err := d.Query(`SELECT node_id, count(*) FROM finding_evidence WHERE node_id IN (`+strings.Join(parts, ",")+`) GROUP BY node_id`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n int64
		var c int
		if rows.Scan(&n, &c) == nil {
			out[n] = c
		}
	}
	return out
}

// GetFindingEvidence fetches a single record (deletion needs the file name + a node ownership check).
func (d *DB) GetFindingEvidence(id int64) (*FindingEvidence, error) {
	var e FindingEvidence
	err := d.QueryRow(`
SELECT id, node_id, filename, caption, step_index, sort_order, created_at
FROM finding_evidence WHERE id=$1`, id).
		Scan(&e.ID, &e.NodeID, &e.Filename, &e.Caption, &e.StepIndex, &e.SortOrder, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// UpdateFindingEvidence edits the caption / step / sort order.
func (d *DB) UpdateFindingEvidence(id int64, caption string, stepIndex, sortOrder int) error {
	_, err := d.Exec(`UPDATE finding_evidence SET caption=$1, step_index=$2, sort_order=$3 WHERE id=$4`,
		strings.TrimSpace(caption), stepIndex, sortOrder, id)
	return err
}

// DeleteFindingEvidence deletes one record (the caller removes the file named in the returned path).
func (d *DB) DeleteFindingEvidence(id int64) error {
	_, err := d.Exec(`DELETE FROM finding_evidence WHERE id=$1`, id)
	return err
}
