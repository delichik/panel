package systeminfo

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/platform/buildinfo"
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
	CheckError      string     `json:"checkError,omitempty"`
}

type Service struct {
	client     *http.Client
	interval   time.Duration
	repository string
	apiBaseURL string
	cancel     context.CancelFunc
	mu         sync.RWMutex
	started    bool
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
		apiBaseURL: "https://api.github.com/repos/",
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
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.mu.Unlock()
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
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.started = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Service) Version() VersionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

func (s *Service) check(ctx context.Context) {
	now := time.Now().UTC()
	markFailed := func(message string) {
		s.mu.Lock()
		s.info.LatestVersion = ""
		s.info.UpdateAvailable = false
		s.info.CheckedAt = &now
		s.info.CheckError = message
		s.mu.Unlock()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBaseURL+s.repository+"/releases/latest", nil)
	if err != nil {
		markFailed("failed to build update check request")
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "panel-update-checker/"+s.info.Version)
	resp, err := s.client.Do(req)
	if err != nil {
		markFailed(err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		markFailed("update check returned HTTP " + strconv.Itoa(resp.StatusCode))
		return
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil || release.Draft || strings.TrimSpace(release.TagName) == "" {
		markFailed("update check returned no valid release")
		return
	}
	s.mu.Lock()
	s.info.LatestVersion = strings.TrimSpace(release.TagName)
	s.info.UpdateAvailable = compareVersions(s.info.LatestVersion, s.info.Version) > 0
	s.info.CheckedAt = &now
	s.info.CheckError = ""
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
