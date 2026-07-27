package certs

import (
	"context"
	"panel/internal/modules/keyassets"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type keyAssetSummaryProvider interface {
	ListSummaries(context.Context) ([]keyassets.Asset, error)
}

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

func (s *Service) ListSelfSigned(ctx context.Context) ([]SelfSignedCertificate, error) {
	if s.keyAssets == nil {
		return nil, panelerr.BadGateway("key_asset_type_invalid", "Key asset service is unavailable")
	}
	var assets []keyassets.Asset
	var err error
	if summaries, ok := s.keyAssets.(keyAssetSummaryProvider); ok {
		assets, err = summaries.ListSummaries(ctx)
	} else {
		assets, err = s.keyAssets.List(ctx)
	}
	if err != nil {
		return nil, err
	}
	out := []SelfSignedCertificate{}
	for _, asset := range assets {
		switch asset.Type {
		case keyassets.TypeCACertificate:
			out = append(out, mapSelfSigned(asset, "ca"))
		case keyassets.TypeTLSCertificate:
			out = append(out, mapSelfSigned(asset, "leaf"))
		}
	}
	return out, nil
}

func (s *Service) GetSelfSigned(ctx context.Context, certID string) (SelfSignedCertificate, error) {
	items, err := s.ListSelfSigned(ctx)
	if err != nil {
		return SelfSignedCertificate{}, err
	}
	for _, item := range items {
		if item.ID == certID {
			return item, nil
		}
	}
	return SelfSignedCertificate{}, panelerr.NotFound("self-signed certificate")
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
	return mapSelfSigned(result.Asset, "leaf"), nil
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
