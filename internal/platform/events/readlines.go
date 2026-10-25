package events

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ReadLines scans recent panel-*.jsonl files under logDir and returns records
// whose eventId (or legacy streamId) matches. Newest files are preferred.
func ReadLines(logDir, eventID string, filter LineFilter) ([]map[string]any, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || strings.TrimSpace(logDir) == "" {
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
	need := after + limit
	var matched []map[string]any
	for _, name := range names {
		if len(matched) >= need {
			break
		}
		path := filepath.Join(logDir, name)
		lines, err := readMatchingLines(path, eventID, need-len(matched), level)
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

func readMatchingLines(path, eventID string, remaining int, level string) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matched []map[string]any
	sc := bufio.NewScanner(f)
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
		id, _ := obj["eventId"].(string)
		if id == "" {
			id, _ = obj["streamId"].(string)
		}
		if id != eventID {
			continue
		}
		if level != "" {
			lv, _ := obj["level"].(string)
			if !levelMatches(strings.ToLower(strings.TrimSpace(lv)), level) {
				continue
			}
		}
		matched = append(matched, obj)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if remaining > 0 && len(matched) > remaining {
		matched = matched[len(matched)-remaining:]
	}
	return matched, nil
}

func levelMatches(got, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return true
	}
	if !strings.Contains(want, ",") {
		return got == want
	}
	for _, part := range strings.Split(want, ",") {
		if got == strings.TrimSpace(part) {
			return true
		}
	}
	return false
}
