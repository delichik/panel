package containerrender

import (
	"bytes"
	"fmt"
	"path"
	"strconv"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

const (
	FileKindBinary   = "binary"
	FileKindTemplate = "template"
)

type Input struct {
	ServiceID          string
	ServiceName        string
	NodeID             string
	Generation         int
	SpecRevision       string
	RootDir            string
	ComposeProjectName string
	ComposeServiceYAML string
	Variables          map[string]string
	Files              []FileInput
}

type FileInput struct {
	Path    string
	Kind    string
	Content []byte
}

type Output struct {
	ComposeYAML  string
	OverrideYAML string
	Manifest     Manifest
	Files        []RenderedFile
}

type Manifest struct {
	Service ManifestService   `json:"service"`
	Labels  map[string]string `json:"labels"`
}

type ManifestService struct {
	Name       string `json:"name"`
	CurrentDir string `json:"currentDir"`
	DataDir    string `json:"dataDir"`
}

type RenderedFile struct {
	RelativePath string
	RemotePath   string
	Content      []byte
}

func Render(in Input) (Output, error) {
	root := strings.TrimRight(firstNonEmpty(in.RootDir, "/opt/panel/container-services"), "/")
	project := firstNonEmpty(in.ComposeProjectName, "panel_managed")
	currentDir := root + "/" + in.ServiceName + "/current"
	dataDir := root + "/" + in.ServiceName + "/data"
	ctx := map[string]any{
		"service": map[string]any{
			"name":        in.ServiceName,
			"generation":  in.Generation,
			"current_dir": currentDir,
			"data_dir":    dataDir,
		},
		"variables": in.Variables,
	}
	body, err := execute("compose", in.ComposeServiceYAML, ctx)
	if err != nil {
		return Output{}, err
	}
	var serviceMap map[string]any
	if err := yaml.Unmarshal([]byte(body), &serviceMap); err != nil {
		return Output{}, err
	}
	composeDoc := map[string]any{"services": map[string]any{in.ServiceName: serviceMap}}
	composeYAML, err := yaml.Marshal(composeDoc)
	if err != nil {
		return Output{}, err
	}
	labels := map[string]string{
		"panel.managed":               "true",
		"panel.service.id":            in.ServiceID,
		"panel.service.name":          in.ServiceName,
		"panel.service.spec_revision": in.SpecRevision,
		"panel.service.generation":    strconv.Itoa(in.Generation),
		"panel.project":               project,
		"panel.node.id":               in.NodeID,
	}
	override := map[string]any{
		"services": map[string]any{
			in.ServiceName: map[string]any{"labels": labels},
		},
	}
	overrideYAML, err := yaml.Marshal(override)
	if err != nil {
		return Output{}, err
	}
	files := []RenderedFile{}
	for _, file := range in.Files {
		rel, err := cleanRel(file.Path)
		if err != nil {
			return Output{}, err
		}
		content := file.Content
		if file.Kind == FileKindTemplate {
			rendered, err := execute(rel, string(file.Content), ctx)
			if err != nil {
				return Output{}, err
			}
			content = []byte(rendered)
		}
		files = append(files, RenderedFile{RelativePath: rel, RemotePath: currentDir + "/files/" + rel, Content: content})
	}
	return Output{
		ComposeYAML:  string(composeYAML),
		OverrideYAML: string(overrideYAML),
		Manifest: Manifest{
			Service: ManifestService{Name: in.ServiceName, CurrentDir: currentDir, DataDir: dataDir},
			Labels:  labels,
		},
		Files: files,
	}, nil
}

func execute(name, source string, ctx any) (string, error) {
	tpl, err := template.New(name).Option("missingkey=error").Parse(source)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, ctx); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func cleanRel(p string) (string, error) {
	p = path.Clean(strings.TrimSpace(strings.ReplaceAll(p, "\\", "/")))
	if p == "." || p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "../") || strings.Contains(p, ":") {
		return "", fmt.Errorf("invalid file path")
	}
	return p, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
