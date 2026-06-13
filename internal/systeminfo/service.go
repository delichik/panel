package systeminfo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"panel/internal/buildinfo"
)

const defaultCheckInterval = 6 * time.Hour

type VersionInfo struct {
	Version         string     `json:"version"`
	Channel         string     `json:"channel"`
	Commit          string     `json:"commit,omitempty"`
	Repository      string     `json:"repository,omitempty"`
	LatestVersion   string     `json:"latestVersion,omitempty"`
	UpdateAvailable bool       `json:"updateAvailable"`
	CheckedAt       *time.Time `json:"checkedAt,omitempty"`
}

type Service struct {
	client     *http.Client
	interval   time.Duration
	repository string
	cancel     context.CancelFunc
	mu         sync.RWMutex
	info       VersionInfo
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Draft   bool   `json:"draft"`
}

func NewService(client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Service{
		client:     client,
		interval:   defaultCheckInterval,
		repository: strings.TrimSpace(buildinfo.Repository),
		info: VersionInfo{
			Version:    buildinfo.NormalizedVersion(),
			Channel:    buildinfo.NormalizedChannel(),
			Commit:     strings.TrimSpace(buildinfo.Commit),
			Repository: strings.TrimSpace(buildinfo.Repository),
		},
	}
}

func (s *Service) Start(parent context.Context) {
	if s.repository == "" || !shouldCheckForUpdates(s.info.Channel, s.info.Version) {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	go func() {
		s.check(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.check(ctx)
			}
		}
	}()
}

func (s *Service) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Service) Version() VersionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

func (s *Service) check(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+s.repository+"/releases/latest", nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "panel-update-checker/"+s.info.Version)
	resp, err := s.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil || release.Draft || strings.TrimSpace(release.TagName) == "" {
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.info.LatestVersion = strings.TrimSpace(release.TagName)
	s.info.UpdateAvailable = compareVersions(s.info.LatestVersion, s.info.Version) > 0
	s.info.CheckedAt = &now
	s.mu.Unlock()
}

func shouldCheckForUpdates(channel, version string) bool {
	return channel == "release" && isReleaseVersion(version)
}

func isReleaseVersion(version string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" || version == "dev" {
		return false
	}
	parts := strings.SplitN(version, "-", 2)
	core := strings.Split(parts[0], ".")
	if len(core) != 3 {
		return false
	}
	for _, part := range core {
		if part == "" || !allDigits(part) {
			return false
		}
	}
	return true
}

func compareVersions(left, right string) int {
	leftParts := versionParts(left)
	rightParts := versionParts(right)
	size := len(leftParts)
	if len(rightParts) > size {
		size = len(rightParts)
	}
	for i := 0; i < size; i++ {
		var l, r int
		if i < len(leftParts) {
			l = leftParts[i]
		}
		if i < len(rightParts) {
			r = rightParts[i]
		}
		if l > r {
			return 1
		}
		if l < r {
			return -1
		}
	}
	return 0
}

func versionParts(version string) []int {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	version = strings.SplitN(version, "-", 2)[0]
	raw := strings.Split(version, ".")
	out := make([]int, 0, len(raw))
	for _, part := range raw {
		value := 0
		for _, char := range part {
			if char < '0' || char > '9' {
				break
			}
			value = value*10 + int(char-'0')
		}
		out = append(out, value)
	}
	return out
}

func allDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
