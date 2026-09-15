package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type revisionRow struct {
	ID, ApplicationID                                                         string
	Generation                                                                int
	SpecHash, RuntimeSpec, Manifest, ImageReference, ResolvedDigest, SpecYAML string
	CreatedAt                                                                 string
}

type rowScanner interface {
	Scan(...any) error
}

func getRevisionTx(ctx context.Context, tx *sql.Tx, applicationID string, generation int) (Revision, error) {
	var r revisionRow
	err := tx.QueryRowContext(ctx, `SELECT id,application_id,generation,spec_hash,rendered_runtime_spec,managed_file_manifest,image_reference,resolved_image_digest,spec_yaml,created_at FROM application_revisions WHERE application_id=? AND generation=?`, applicationID, generation).
		Scan(&r.ID, &r.ApplicationID, &r.Generation, &r.SpecHash, &r.RuntimeSpec, &r.Manifest, &r.ImageReference, &r.ResolvedDigest, &r.SpecYAML, &r.CreatedAt)
	return revisionFromRow(r), err
}

func revisionFromRow(r revisionRow) Revision {
	var manifest []map[string]any
	_ = json.Unmarshal([]byte(firstJSON(r.Manifest, "[]")), &manifest)
	return Revision{ID: r.ID, ApplicationID: r.ApplicationID, Generation: r.Generation, SpecHash: r.SpecHash,
		RenderedRuntimeSpec: json.RawMessage(firstJSON(r.RuntimeSpec, "{}")), ManagedFileManifest: manifest, ImageReference: r.ImageReference,
		ResolvedImageDigest: r.ResolvedDigest, SpecYAML: r.SpecYAML, CreatedAt: parseTimeValue(r.CreatedAt)}
}

func (s *Store) GetRevision(ctx context.Context, applicationID string, generation int) (Revision, error) {
	var r revisionRow
	err := s.db.QueryRowContext(ctx, `SELECT id,application_id,generation,spec_hash,rendered_runtime_spec,managed_file_manifest,image_reference,resolved_image_digest,spec_yaml,created_at FROM application_revisions WHERE application_id=? AND generation=?`, applicationID, generation).
		Scan(&r.ID, &r.ApplicationID, &r.Generation, &r.SpecHash, &r.RuntimeSpec, &r.Manifest, &r.ImageReference, &r.ResolvedDigest, &r.SpecYAML, &r.CreatedAt)
	if err != nil {
		return Revision{}, err
	}
	if strings.TrimSpace(r.RuntimeSpec) == "" {
		r.RuntimeSpec = "{}"
	}
	if strings.TrimSpace(r.Manifest) == "" {
		r.Manifest = "[]"
	}
	return revisionFromRow(r), nil
}

func (s *Store) GetRevisionByID(ctx context.Context, revisionID string) (Revision, error) {
	var r revisionRow
	err := s.db.QueryRowContext(ctx, `SELECT id,application_id,generation,spec_hash,rendered_runtime_spec,managed_file_manifest,image_reference,resolved_image_digest,spec_yaml,created_at FROM application_revisions WHERE id=?`, revisionID).
		Scan(&r.ID, &r.ApplicationID, &r.Generation, &r.SpecHash, &r.RuntimeSpec, &r.Manifest, &r.ImageReference, &r.ResolvedDigest, &r.SpecYAML, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, err
	}
	if err != nil {
		return Revision{}, err
	}
	return revisionFromRow(r), nil
}

func revisionNow() time.Time { return time.Now().UTC() }
