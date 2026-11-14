package certs

import (
	"context"
	"strings"
	"time"

	"panel/internal/modules/keyassets"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type keyAssetSummaryPageProvider interface {
	ListSummaryPageByTypes(context.Context, int, int, string, []string) (httpx.ListPage[keyassets.Asset], error)
}

func (s *Service) ListSelfSignedPage(ctx context.Context, page, pageSize int, query string) (httpx.ListPage[SelfSignedCertificate], error) {
	provider, ok := s.keyAssets.(keyAssetSummaryPageProvider)
	if !ok {
		return httpx.ListPage[SelfSignedCertificate]{}, panelerr.BadGateway("key_asset_type_invalid", "Key asset summary service is unavailable")
	}
	assets, err := provider.ListSummaryPageByTypes(ctx, page, pageSize, query, []string{keyassets.TypeCACertificate, keyassets.TypeTLSCertificate})
	if err != nil {
		return httpx.ListPage[SelfSignedCertificate]{}, err
	}
	items := make([]SelfSignedCertificate, 0, len(assets.Items))
	for _, asset := range assets.Items {
		kind := "leaf"
		if asset.Type == keyassets.TypeCACertificate {
			kind = "ca"
		}
		items = append(items, mapSelfSigned(asset, kind))
	}
	return httpx.ListPage[SelfSignedCertificate]{Items: items, Total: assets.Total, Page: assets.Page, PageSize: assets.PageSize}, nil
}

func (s *Service) CreateSelfSignedCA(ctx context.Context, in SelfSignedCARequest) (SelfSignedCertificate, error) {
	if s.keyAssets == nil {
		return SelfSignedCertificate{}, panelerr.BadGateway("key_asset_type_invalid", "Key asset service is unavailable")
	}
	asset, err := s.keyAssets.CreateCA(ctx, keyassets.CreateCARequest{
		Name:       in.Name,
		CommonName: in.CommonName,
		Algorithm:  keyassets.AlgorithmEd25519,
		Years:      in.Years,
	})
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	return mapSelfSigned(asset, "ca"), nil
}

func (s *Service) CreateSelfSignedLeaf(ctx context.Context, in SelfSignedLeafRequest) (SelfSignedCertificate, error) {
	if s.keyAssets == nil {
		return SelfSignedCertificate{}, panelerr.BadGateway("key_asset_type_invalid", "Key asset service is unavailable")
	}
	asset, err := s.keyAssets.CreateTLS(ctx, keyassets.CreateTLSRequest{
		Name:          in.Name,
		ParentAssetID: in.CAID,
		CommonName:    in.CommonName,
		Algorithm:     keyassets.AlgorithmEd25519,
		DNSNames:      in.DNSNames,
		IPAddresses:   in.IPAddresses,
		Days:          in.Days,
	})
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	return mapSelfSigned(asset, "leaf"), nil
}

func (s *Service) RenewSelfSignedLeaf(ctx context.Context, certID string) (SelfSignedCertificate, error) {
	if s.keyAssets == nil {
		return SelfSignedCertificate{}, panelerr.BadGateway("key_asset_type_invalid", "Key asset service is unavailable")
	}
	result, err := s.keyAssets.ReissueTLS(ctx, certID)
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	if err := s.waitKeyAssetTask(ctx, result.TaskID); err != nil {
		return SelfSignedCertificate{}, err
	}
	asset, err := s.keyAssets.Get(ctx, certID)
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	return mapSelfSigned(asset, "leaf"), nil
}

// waitKeyAssetTask blocks until the registered executor finishes. Self-signed
// renew stays a synchronous 200 that returns the new certificate; the key-asset
// reissue HTTP API itself stays 202 and returns the pre-task snapshot.
func (s *Service) waitKeyAssetTask(ctx context.Context, taskID string) error {
	if s == nil || s.tasks == nil || strings.TrimSpace(taskID) == "" {
		return panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		task, err := s.tasks.Get(ctx, taskID)
		if err != nil {
			return err
		}
		switch task.Status {
		case tasks.StatusCompleted:
			return nil
		case tasks.StatusFailed, tasks.StatusFailedRetryable, tasks.StatusBlocked, tasks.StatusCancelled:
			msg := strings.TrimSpace(task.Error)
			if msg == "" {
				msg = "Self-signed certificate renewal did not complete"
			}
			return panelerr.BadGateway("key_asset_reissue_failed", msg)
		}
		if !time.Now().Before(deadline) {
			return panelerr.BadGateway("key_asset_reissue_failed", "Self-signed certificate renewal did not finish in time")
		}
		timer := time.NewTimer(15 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) DeleteSelfSigned(ctx context.Context, certID string) error {
	if s.keyAssets == nil {
		return panelerr.BadGateway("key_asset_type_invalid", "Key asset service is unavailable")
	}
	return s.keyAssets.Delete(ctx, certID)
}

func mapSelfSigned(asset keyassets.Asset, kind string) SelfSignedCertificate {
	return SelfSignedCertificate{
		ID:          asset.ID,
		ParentCAID:  asset.ParentAssetID,
		Kind:        kind,
		Name:        asset.Name,
		CommonName:  asset.CommonName,
		DNSNames:    append([]string(nil), asset.DNSNames...),
		IPAddresses: append([]string(nil), asset.IPAddresses...),
		Fingerprint: asset.Fingerprint,
		NotBefore:   asset.NotBefore,
		NotAfter:    asset.NotAfter,
		CreatedAt:   asset.CreatedAt,
		UpdatedAt:   asset.UpdatedAt,
	}
}
