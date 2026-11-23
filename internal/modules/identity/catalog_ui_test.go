package auth

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"panel/internal/modules/identity/permissioncatalog"
)

var anchorPattern = regexp.MustCompile(`data-perm-anchor="([^"]+)"`)

func TestCatalogUIMappingMatchesSourceAnchors(t *testing.T) {
	permissions := permissioncatalog.Permissions()
	catalogAnchors := map[string]string{}
	actionable := map[string]bool{
		"button": true, "menu": true, "field": true, "column": true, "tab": true, "dialog": true, "mask": true,
	}
	for _, permission := range permissions {
		if len(permission.UI) == 0 {
			t.Errorf("permission %q has no UI mapping", permission.Key)
		}
		for _, target := range permission.UI {
			if target.Page == "" || target.Kind == "" || target.Effect == "" {
				t.Errorf("permission %q has incomplete UI target: %#v", permission.Key, target)
			}
			if actionable[target.Kind] && target.Anchor == "" {
				t.Errorf("permission %q kind %q must declare an anchor", permission.Key, target.Kind)
			}
			if target.Anchor == "" {
				continue
			}
			if previous, exists := catalogAnchors[target.Anchor]; exists {
				t.Errorf("anchor %q declared by both %q and %q", target.Anchor, previous, permission.Key)
			}
			catalogAnchors[target.Anchor] = permission.Key
		}
	}

	sourceAnchors := sourceAnchors(t)
	for anchor := range sourceAnchors {
		if _, ok := catalogAnchors[anchor]; !ok {
			t.Errorf("source anchor %q is not declared in the permission catalog", anchor)
		}
	}
	for anchor := range catalogAnchors {
		if !sourceAnchors[anchor] {
			t.Errorf("catalog anchor %q is missing from Vue sources", anchor)
		}
	}
}

func sourceAnchors(t *testing.T) map[string]bool {
	t.Helper()
	root := repositoryRoot(t)
	found := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "web", "src"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".vue") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range anchorPattern.FindAllStringSubmatch(string(content), -1) {
			found[match[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestCatalogUIMappingIsStable(t *testing.T) {
	// Guard against accidental anchor renames: sorted anchor list is fixed.
	permissions := permissioncatalog.Permissions()
	var anchors []string
	for _, permission := range permissions {
		for _, target := range permission.UI {
			if target.Anchor != "" {
				anchors = append(anchors, target.Anchor)
			}
		}
	}
	sort.Strings(anchors)
	want := []string{
		"applications.deploy.sync", "applications.edit.create", "certificates.manage.issue",
		"credentials.manage.create", "debug.manage.clear", "dns.manage.recordCreate",
		"events.export.download", "events.manage.clear", "facilities.manage.pathAdd",
		"keyassets.export.download", "keyassets.manage.generate", "servers.execute.restart",
		"servers.manage.create", "settings.manage.remoteTimeout", "settings.security.jwtReset",
	}
	if strings.Join(anchors, ",") != strings.Join(want, ",") {
		t.Fatalf("catalog anchors changed:\n got: %v\nwant: %v", anchors, want)
	}
}
