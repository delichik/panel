package logcontexts

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ReadLines scans recent panel-*.jsonl files under logDir and returns records
// whose streamId matches. Newest files are preferred; Limit caps the result count.
func ReadLines(logDir, streamID string, filter LineFilter) ([]map[string]any, error) {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" || strings.TrimSpace(logDir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "panel-") && strings.HasSuffix(name, ".jsonl") {
			names = append(names, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	limit := filter.Limit
	if limit <= 0 {
		limit = 1000
	}
	after := filter.After
	if after < 0 {
		after = 0
	}
	level := strings.ToLower(strings.TrimSpace(filter.Level))
	// Collect enough matching lines to satisfy after+limit (newest-first across files).
	need := after + limit
	var matched []map[string]any
	for _, name := range names {
		if len(matched) >= need {
			break
		}
		path := filepath.Join(logDir, name)
		lines, err := readMatchingLines(path, streamID, need-len(matched), level)
		if err != nil {
			return matched, err
		}
		matched = append(matched, lines...)
	}
	if after >= len(matched) {
		return nil, nil
	}
	end := after + limit
	if end > len(matched) {
		end = len(matched)
	}
	return matched[after:end], nil
}

func readMatchingLines(path, streamID string, remaining int, level string) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matched []map[string]any
	sc := bufio.NewScanner(f)
	// Allow long JSON lines (default 64K is often too small for structured logs).
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 2*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		id, _ := obj["streamId"].(string)
		if id != streamID {
			continue
		}
		if level != "" {
			lv, _ := obj["level"].(string)
			if strings.ToLower(strings.TrimSpace(lv)) != level {
				continue
			}
		}
		matched = append(matched, obj)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if remaining > 0 && len(matched) > remaining {
		// Prefer newest lines within the file (file is append-only).
		matched = matched[len(matched)-remaining:]
	}
	return matched, nil
}
