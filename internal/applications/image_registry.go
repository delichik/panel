package applications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"panel/internal/panelerr"
)

const manifestAcceptHeader = "application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json"

type ImageDigestResult struct {
	Reference  string `json:"reference"`
	Registry   string `json:"registry,omitempty"`
	Repository string `json:"repository,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Digest     string `json:"digest"`
	Pinned     bool   `json:"pinned"`
}

type RegistryImageResolver struct {
	Client *http.Client
	Scheme string
}

func NewRegistryImageResolver() *RegistryImageResolver {
	return &RegistryImageResolver{
		Client: &http.Client{Timeout: 12 * time.Second},
		Scheme: "https",
	}
}

func (r *RegistryImageResolver) Resolve(ctx context.Context, image string) (ImageDigestResult, error) {
	ref, err := parseImageReference(image)
	if err != nil {
		return ImageDigestResult{}, err
	}
	if ref.Pinned {
		return ref, nil
	}
	digest, err := r.manifestDigest(ctx, ref, "")
	if err != nil {
		return ImageDigestResult{}, err
	}
	ref.Digest = digest
	return ref, nil
}

func (r *RegistryImageResolver) manifestDigest(ctx context.Context, ref ImageDigestResult, token string) (string, error) {
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	scheme := strings.TrimSpace(r.Scheme)
	if scheme == "" {
		scheme = "https"
	}
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, ref.Registry, ref.Repository, url.PathEscape(ref.Tag))
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, manifestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", manifestAcceptHeader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", panelerr.BadGateway("image_registry_unreachable", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && token == "" {
		token, err := r.fetchBearerToken(ctx, resp.Header.Get("WWW-Authenticate"))
		if err != nil {
			return "", panelerr.BadGateway("image_registry_auth_error", err.Error())
		}
		return r.manifestDigest(ctx, ref, token)
	}
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.Header.Get("Docker-Content-Digest") == "" {
		return r.manifestDigestWithGet(ctx, ref, token)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", panelerr.BadGateway("image_registry_error", fmt.Sprintf("registry returned %s for %s", resp.Status, ref.Reference))
	}
	return resp.Header.Get("Docker-Content-Digest"), nil
}

func (r *RegistryImageResolver) manifestDigestWithGet(ctx context.Context, ref ImageDigestResult, token string) (string, error) {
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	scheme := strings.TrimSpace(r.Scheme)
	if scheme == "" {
		scheme = "https"
	}
	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, ref.Registry, ref.Repository, url.PathEscape(ref.Tag))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", manifestAcceptHeader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", panelerr.BadGateway("image_registry_unreachable", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && token == "" {
		token, err := r.fetchBearerToken(ctx, resp.Header.Get("WWW-Authenticate"))
		if err != nil {
			return "", panelerr.BadGateway("image_registry_auth_error", err.Error())
		}
		return r.manifestDigestWithGet(ctx, ref, token)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", panelerr.BadGateway("image_registry_error", fmt.Sprintf("registry returned %s for %s", resp.Status, ref.Reference))
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", panelerr.BadGateway("image_registry_missing_digest", "registry response did not include Docker-Content-Digest")
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return digest, nil
}

func (r *RegistryImageResolver) fetchBearerToken(ctx context.Context, challenge string) (string, error) {
	realm, params, err := parseBearerChallenge(challenge)
	if err != nil {
		return "", err
	}
	tokenURL, err := url.Parse(realm)
	if err != nil {
		return "", err
	}
	query := tokenURL.Query()
	for key, value := range params {
		if key == "realm" || value == "" {
			continue
		}
		query.Set(key, value)
	}
	tokenURL.RawQuery = query.Encode()
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", panelerr.BadGateway("image_registry_auth_unreachable", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", panelerr.BadGateway("image_registry_auth_error", fmt.Sprintf("registry auth returned %s", resp.Status))
	}
	var out struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Token != "" {
		return out.Token, nil
	}
	if out.AccessToken != "" {
		return out.AccessToken, nil
	}
	return "", panelerr.BadGateway("image_registry_auth_error", "registry auth response did not include a token")
}

func parseImageReference(image string) (ImageDigestResult, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return ImageDigestResult{}, panelerr.Validation("application_image_empty", "application image is empty")
	}
	base, digest, pinned := strings.Cut(image, "@")
	if pinned {
		digest = strings.TrimSpace(digest)
		if digest == "" {
			return ImageDigestResult{}, panelerr.Validation("application_image_invalid", "image digest is empty")
		}
		return ImageDigestResult{Reference: image, Digest: digest, Pinned: true}, nil
	}
	tag := "latest"
	lastSlash := strings.LastIndex(base, "/")
	lastColon := strings.LastIndex(base, ":")
	if lastColon > lastSlash {
		tag = base[lastColon+1:]
		base = base[:lastColon]
	}
	if tag == "" {
		return ImageDigestResult{}, panelerr.Validation("application_image_invalid", "image tag is empty")
	}
	parts := strings.Split(base, "/")
	registry := "registry-1.docker.io"
	repository := base
	if len(parts) > 1 && isRegistryHost(parts[0]) {
		registry = parts[0]
		repository = strings.Join(parts[1:], "/")
	}
	if repository == "" {
		return ImageDigestResult{}, panelerr.Validation("application_image_invalid", "image repository is empty")
	}
	if registry == "registry-1.docker.io" && !strings.Contains(repository, "/") {
		repository = "library/" + repository
	}
	return ImageDigestResult{
		Reference:  registry + "/" + repository + ":" + tag,
		Registry:   registry,
		Repository: repository,
		Tag:        tag,
	}, nil
}

func isRegistryHost(value string) bool {
	return strings.Contains(value, ".") || strings.Contains(value, ":") || value == "localhost"
}

func parseBearerChallenge(challenge string) (string, map[string]string, error) {
	scheme, rest, ok := strings.Cut(strings.TrimSpace(challenge), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", nil, errors.New("registry did not provide a bearer auth challenge")
	}
	params := map[string]string{}
	for _, part := range splitChallengeParams(rest) {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		params[key] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	realm := params["realm"]
	if realm == "" {
		return "", nil, errors.New("registry auth challenge did not include a realm")
	}
	return realm, params, nil
}

func splitChallengeParams(value string) []string {
	var parts []string
	start := 0
	quoted := false
	for i, r := range value {
		switch r {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				parts = append(parts, value[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, value[start:])
	return parts
}
