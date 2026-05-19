package compose

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"panel/internal/id"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/sshx"
	"panel/internal/tasks"
)

type Service struct {
	db       *sql.DB
	dataRoot string
	servers  *server.Service
	tasks    *tasks.Service
	exec     sshx.RemoteExecutor
}

func NewService(db *sql.DB, dataRoot string, servers *server.Service, taskSvc *tasks.Service, exec ...sshx.RemoteExecutor) *Service {
	var remote sshx.RemoteExecutor
	if len(exec) > 0 {
		remote = exec[0]
	}
	return &Service{db: db, dataRoot: dataRoot, servers: servers, tasks: taskSvc, exec: remote}
}

func (s *Service) ListTemplates(ctx context.Context) ([]ServiceTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,compose_yaml,visual_state,variables,version,created_at,updated_at FROM service_templates ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceTemplate{}
	for rows.Next() {
		tpl, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tpl)
	}
	return out, rows.Err()
}

func (s *Service) CreateTemplate(ctx context.Context, req SaveTemplateRequest) (ServiceTemplate, error) {
	if err := validateTemplate(req); err != nil {
		return ServiceTemplate{}, err
	}
	now := time.Now().UTC()
	tpl := ServiceTemplate{ID: id.New("tmpl"), Name: strings.TrimSpace(req.Name), Description: req.Description, ComposeYAML: req.ComposeYAML, VisualState: nonNilMap(req.VisualState), Variables: req.Variables, Version: 1, CreatedAt: now, UpdatedAt: now}
	visual, _ := json.Marshal(tpl.VisualState)
	vars, _ := json.Marshal(tpl.Variables)
	_, err := s.db.ExecContext(ctx, `INSERT INTO service_templates(id,name,description,compose_yaml,visual_state,variables,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		tpl.ID, tpl.Name, tpl.Description, tpl.ComposeYAML, string(visual), string(vars), tpl.Version, ts(now), ts(now))
	if err != nil {
		return ServiceTemplate{}, err
	}
	return tpl, nil
}

func (s *Service) GetTemplate(ctx context.Context, templateID string) (ServiceTemplate, error) {
	tpl, err := scanTemplate(s.db.QueryRowContext(ctx, `SELECT id,name,description,compose_yaml,visual_state,variables,version,created_at,updated_at FROM service_templates WHERE id=?`, templateID))
	if err == sql.ErrNoRows {
		return ServiceTemplate{}, panelerr.NotFound("service_template")
	}
	return tpl, err
}

func (s *Service) UpdateTemplate(ctx context.Context, templateID string, req SaveTemplateRequest) (ServiceTemplate, error) {
	if err := validateTemplate(req); err != nil {
		return ServiceTemplate{}, err
	}
	old, err := s.GetTemplate(ctx, templateID)
	if err != nil {
		return ServiceTemplate{}, err
	}
	now := time.Now().UTC()
	visual, _ := json.Marshal(nonNilMap(req.VisualState))
	vars, _ := json.Marshal(req.Variables)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ServiceTemplate{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE service_templates SET name=?,description=?,compose_yaml=?,visual_state=?,variables=?,version=?,updated_at=? WHERE id=?`,
		strings.TrimSpace(req.Name), req.Description, req.ComposeYAML, string(visual), string(vars), old.Version+1, ts(now), templateID); err != nil {
		_ = tx.Rollback()
		return ServiceTemplate{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE deployed_services SET drifted=1,updated_at=? WHERE template_id=?`, ts(now), templateID); err != nil {
		_ = tx.Rollback()
		return ServiceTemplate{}, err
	}
	if err := tx.Commit(); err != nil {
		return ServiceTemplate{}, err
	}
	return s.GetTemplate(ctx, templateID)
}

func (s *Service) DeleteTemplate(ctx context.Context, templateID string) error {
	var linked int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM deployed_services WHERE template_id=?`, templateID).Scan(&linked); err != nil {
		return err
	}
	if linked > 0 {
		return panelerr.Conflict("service_template_in_use", "Service template has linked services")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM service_templates WHERE id=?`, templateID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("service_template")
	}
	return os.RemoveAll(filepath.Join(s.dataRoot, "service_templates", templateID))
}

func (s *Service) ListTemplateFiles(ctx context.Context, templateID string) ([]TemplateFile, error) {
	if _, err := s.GetTemplate(ctx, templateID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,template_id,path,kind,content_type,size,sha256,created_at,updated_at FROM service_template_files WHERE template_id=? ORDER BY path ASC`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TemplateFile{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		if err := s.attachTemplateFileContent(templateID, &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Service) CreateTemplateFile(ctx context.Context, templateID, kind string, req SaveFileRequest) (TemplateFile, error) {
	if _, err := s.GetTemplate(ctx, templateID); err != nil {
		return TemplateFile{}, err
	}
	if kind != FileKindBinary && kind != FileKindTemplate {
		return TemplateFile{}, panelerr.Validation("file_kind_invalid", "File kind must be binary or template")
	}
	cleanPath, err := cleanRelativePath(req.Path)
	if err != nil {
		return TemplateFile{}, err
	}
	content, err := requestContent(req, kind)
	if err != nil {
		return TemplateFile{}, err
	}
	if kind == FileKindTemplate {
		if _, err := parseStrictTemplate(cleanPath, string(content)); err != nil {
			return TemplateFile{}, panelerr.Validation("template_file_invalid", err.Error())
		}
	}
	now := time.Now().UTC()
	f := TemplateFile{ID: id.New("file"), TemplateID: templateID, Path: cleanPath, Kind: kind, ContentType: req.ContentType, Size: int64(len(content)), SHA256: checksum(content), CreatedAt: now, UpdatedAt: now}
	if kind == FileKindTemplate {
		f.Content = string(content)
	} else {
		f.Base64 = base64.StdEncoding.EncodeToString(content)
	}
	if err := s.writeTemplateFile(templateID, f.ID, kind, content); err != nil {
		return TemplateFile{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO service_template_files(id,template_id,path,kind,content_type,size,sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		f.ID, f.TemplateID, f.Path, f.Kind, f.ContentType, f.Size, f.SHA256, ts(now), ts(now))
	if err == nil {
		err = s.bumpTemplateVersion(ctx, templateID)
	}
	return f, err
}

func (s *Service) UpdateTemplateFile(ctx context.Context, templateID, fileID string, req SaveFileRequest) (TemplateFile, error) {
	old, err := s.GetTemplateFile(ctx, templateID, fileID)
	if err != nil {
		return TemplateFile{}, err
	}
	cleanPath, err := cleanRelativePath(firstNonEmpty(req.Path, old.Path))
	if err != nil {
		return TemplateFile{}, err
	}
	content, err := requestContent(req, old.Kind)
	if err != nil {
		return TemplateFile{}, err
	}
	if old.Kind == FileKindTemplate {
		if _, err := parseStrictTemplate(cleanPath, string(content)); err != nil {
			return TemplateFile{}, panelerr.Validation("template_file_invalid", err.Error())
		}
	}
	now := time.Now().UTC()
	if err := s.writeTemplateFile(templateID, fileID, old.Kind, content); err != nil {
		return TemplateFile{}, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE service_template_files SET path=?,content_type=?,size=?,sha256=?,updated_at=? WHERE id=? AND template_id=?`,
		cleanPath, firstNonEmpty(req.ContentType, old.ContentType), len(content), checksum(content), ts(now), fileID, templateID)
	if err != nil {
		return TemplateFile{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return TemplateFile{}, panelerr.NotFound("template_file")
	}
	if err := s.bumpTemplateVersion(ctx, templateID); err != nil {
		return TemplateFile{}, err
	}
	return s.GetTemplateFile(ctx, templateID, fileID)
}

func (s *Service) GetTemplateFile(ctx context.Context, templateID, fileID string) (TemplateFile, error) {
	f, err := scanFile(s.db.QueryRowContext(ctx, `SELECT id,template_id,path,kind,content_type,size,sha256,created_at,updated_at FROM service_template_files WHERE id=? AND template_id=?`, fileID, templateID))
	if err == sql.ErrNoRows {
		return TemplateFile{}, panelerr.NotFound("template_file")
	}
	if err != nil {
		return TemplateFile{}, err
	}
	if err := s.attachTemplateFileContent(templateID, &f); err != nil {
		return TemplateFile{}, err
	}
	return f, nil
}

func (s *Service) DeleteTemplateFile(ctx context.Context, templateID, fileID string) error {
	f, err := s.GetTemplateFile(ctx, templateID, fileID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM service_template_files WHERE id=? AND template_id=?`, fileID, templateID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("template_file")
	}
	if err := os.Remove(s.templateFilePath(templateID, fileID, f.Kind)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return s.bumpTemplateVersion(ctx, templateID)
}

func (s *Service) ServerVariables(ctx context.Context, serverID string) (map[string]any, error) {
	if _, err := s.servers.Get(ctx, serverID); err != nil {
		return nil, err
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT variables FROM server_variables WHERE server_id=?`, serverID).Scan(&raw)
	if err == sql.ErrNoRows {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out, nil
}

func (s *Service) PutServerVariables(ctx context.Context, serverID string, vars map[string]any) (map[string]any, error) {
	if _, err := s.servers.Get(ctx, serverID); err != nil {
		return nil, err
	}
	if vars == nil {
		vars = map[string]any{}
	}
	raw, _ := json.Marshal(vars)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO server_variables(server_id,variables,updated_at) VALUES(?,?,?)
		ON CONFLICT(server_id) DO UPDATE SET variables=excluded.variables,updated_at=excluded.updated_at`, serverID, string(raw), now)
	if err != nil {
		return nil, err
	}
	return vars, nil
}

func (s *Service) ListServices(ctx context.Context) ([]DeployedService, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,server_id,template_id,template_version,remote_path,values_json,labels_json,status,drifted,last_task_id,created_at,updated_at FROM deployed_services ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeployedService{}
	for rows.Next() {
		svc, err := scanDeployedService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

func (s *Service) ListTemplateServices(ctx context.Context, templateID string) ([]DeployedService, error) {
	if _, err := s.GetTemplate(ctx, templateID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,server_id,template_id,template_version,remote_path,values_json,labels_json,status,drifted,last_task_id,created_at,updated_at FROM deployed_services WHERE template_id=? ORDER BY updated_at DESC`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeployedService{}
	for rows.Next() {
		svc, err := scanDeployedService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

func (s *Service) CreateService(ctx context.Context, req SaveServiceRequest) (DeployedService, error) {
	if err := s.validateServiceRequest(ctx, req); err != nil {
		return DeployedService{}, err
	}
	tpl, err := s.GetTemplate(ctx, req.TemplateID)
	if err != nil {
		return DeployedService{}, err
	}
	now := time.Now().UTC()
	svc := DeployedService{
		ID:              id.New("svc"),
		Name:            strings.TrimSpace(req.Name),
		ServerID:        req.ServerID,
		TemplateID:      req.TemplateID,
		TemplateVersion: tpl.Version,
		RemotePath:      strings.TrimSpace(req.RemotePath),
		Values:          nonNilMap(req.Values),
		Labels:          serviceLabels("", req.ServerID, req.TemplateID, tpl.Version),
		Status:          ServiceStatusDraft,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	svc.Labels["panel.service_id"] = svc.ID
	values, _ := json.Marshal(svc.Values)
	labels, _ := json.Marshal(svc.Labels)
	_, err = s.db.ExecContext(ctx, `INSERT INTO deployed_services(id,name,server_id,template_id,template_version,remote_path,values_json,labels_json,status,drifted,last_task_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		svc.ID, svc.Name, svc.ServerID, svc.TemplateID, svc.TemplateVersion, svc.RemotePath, string(values), string(labels), svc.Status, 0, "", ts(now), ts(now))
	return svc, err
}

func (s *Service) GetService(ctx context.Context, serviceID string) (DeployedService, error) {
	svc, err := scanDeployedService(s.db.QueryRowContext(ctx, `SELECT id,name,server_id,template_id,template_version,remote_path,values_json,labels_json,status,drifted,last_task_id,created_at,updated_at FROM deployed_services WHERE id=?`, serviceID))
	if err == sql.ErrNoRows {
		return DeployedService{}, panelerr.NotFound("service")
	}
	return svc, err
}

func (s *Service) UpdateService(ctx context.Context, serviceID string, req SaveServiceRequest) (DeployedService, error) {
	if err := s.validateServiceRequest(ctx, req); err != nil {
		return DeployedService{}, err
	}
	tpl, err := s.GetTemplate(ctx, req.TemplateID)
	if err != nil {
		return DeployedService{}, err
	}
	now := time.Now().UTC()
	values, _ := json.Marshal(nonNilMap(req.Values))
	labels := serviceLabels(serviceID, req.ServerID, req.TemplateID, tpl.Version)
	labelsJSON, _ := json.Marshal(labels)
	drifted := 0
	old, err := s.GetService(ctx, serviceID)
	if err != nil {
		return DeployedService{}, err
	}
	templateVersion := tpl.Version
	if old.TemplateID == req.TemplateID {
		templateVersion = old.TemplateVersion
		if old.TemplateVersion < tpl.Version {
			drifted = 1
		}
	}
	res, err := s.db.ExecContext(ctx, `UPDATE deployed_services SET name=?,server_id=?,template_id=?,template_version=?,remote_path=?,values_json=?,labels_json=?,drifted=?,updated_at=? WHERE id=?`,
		strings.TrimSpace(req.Name), req.ServerID, req.TemplateID, templateVersion, strings.TrimSpace(req.RemotePath), string(values), string(labelsJSON), drifted, ts(now), serviceID)
	if err != nil {
		return DeployedService{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return DeployedService{}, panelerr.NotFound("service")
	}
	return s.GetService(ctx, serviceID)
}

func (s *Service) DeleteService(ctx context.Context, serviceID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM deployed_services WHERE id=?`, serviceID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("service")
	}
	return nil
}

func (s *Service) RenderTemplate(ctx context.Context, templateID string, req RenderRequest) (RenderResult, error) {
	tpl, err := s.GetTemplate(ctx, templateID)
	if err != nil {
		return RenderResult{}, err
	}
	return s.render(ctx, tpl, req)
}

func (s *Service) RenderService(ctx context.Context, serviceID string) (RenderResult, error) {
	svc, err := s.GetService(ctx, serviceID)
	if err != nil {
		return RenderResult{}, err
	}
	tpl, err := s.GetTemplate(ctx, svc.TemplateID)
	if err != nil {
		return RenderResult{}, err
	}
	return s.render(ctx, tpl, RenderRequest{ServerID: svc.ServerID, ServiceID: svc.ID, ServiceName: svc.Name, RemotePath: svc.RemotePath, Values: svc.Values})
}

func (s *Service) ValidateTemplate(ctx context.Context, templateID string, req RenderRequest) error {
	_, err := s.RenderTemplate(ctx, templateID, req)
	return err
}

func (s *Service) LifecycleTask(ctx context.Context, serviceID, op string) (tasks.Task, error) {
	svc, err := s.GetService(ctx, serviceID)
	if err != nil {
		return tasks.Task{}, err
	}
	switch op {
	case "deploy", "sync":
		if _, err := s.RenderService(ctx, serviceID); err != nil {
			return tasks.Task{}, err
		}
	case "restart", "stop", "remove", "update-images":
	default:
		return tasks.Task{}, panelerr.NotFound("route")
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{Type: "compose_service_" + strings.ReplaceAll(op, "-", "_"), ServerID: svc.ServerID, Summary: "Compose service " + op + " local metadata workflow"})
	if err != nil {
		return tasks.Task{}, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE deployed_services SET last_task_id=?,updated_at=? WHERE id=?`, task.ID, ts(time.Now().UTC()), serviceID)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runLocalLifecycle(context.Background(), task.ID, serviceID, op)
	return task, nil
}

func (s *Service) runLocalLifecycle(ctx context.Context, taskID, serviceID, op string) {
	_ = s.tasks.Start(ctx, taskID)
	_ = s.tasks.Advance(ctx, taskID, "validating", "local metadata closed loop: validating template and service values")
	if op == "deploy" || op == "sync" {
		if _, err := s.RenderService(ctx, serviceID); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		_ = s.tasks.Advance(ctx, taskID, "rendering", "local metadata closed loop: rendered compose YAML and template files locally")
		if err := s.persistRendered(ctx, serviceID); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		if s.exec != nil {
			_ = s.tasks.Advance(ctx, taskID, "uploading", "uploading Compose project files")
			if err := s.uploadAndApply(ctx, serviceID); err != nil {
				_ = s.tasks.Fail(ctx, taskID, err)
				return
			}
		} else {
			_ = s.tasks.Advance(ctx, taskID, "remote_pending", "remote docker compose execution is not connected yet; no SSH deploy was performed")
		}
		if err := s.markApplied(ctx, serviceID, op); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		_ = s.tasks.Complete(ctx, taskID, "Compose service "+op+" completed")
		return
	}
	if s.exec != nil {
		_ = s.tasks.Advance(ctx, taskID, "remote", "running docker compose "+op)
		if err := s.runRemoteLifecycle(ctx, serviceID, op); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
	} else {
		_ = s.tasks.Advance(ctx, taskID, "remote_pending", "local metadata closed loop only; remote "+op+" execution is pending integration")
	}
	if op == "remove" {
		_, _ = s.db.ExecContext(ctx, `UPDATE deployed_services SET status=?,updated_at=? WHERE id=?`, ServiceStatusRemoved, ts(time.Now().UTC()), serviceID)
	}
	_ = s.tasks.Complete(ctx, taskID, "Compose service "+op+" completed")
}

func (s *Service) uploadAndApply(ctx context.Context, serviceID string) error {
	svc, err := s.GetService(ctx, serviceID)
	if err != nil {
		return err
	}
	srv, err := s.servers.Get(ctx, svc.ServerID)
	if err != nil {
		return err
	}
	rendered, err := s.RenderService(ctx, serviceID)
	if err != nil {
		return err
	}
	if err := s.remoteMkdir(ctx, srv.Target(), svc.RemotePath); err != nil {
		return err
	}
	if err := s.remoteWrite(ctx, srv.Target(), filepath.Join(svc.RemotePath, "compose.yaml"), []byte(rendered.ComposeYAML)); err != nil {
		return err
	}
	for _, file := range rendered.Files {
		remotePath := filepath.ToSlash(filepath.Join(svc.RemotePath, file.Path))
		if err := s.remoteMkdir(ctx, srv.Target(), filepath.Dir(remotePath)); err != nil {
			return err
		}
		content := []byte(file.Content)
		if file.Kind == FileKindBinary {
			tpl, err := s.GetTemplate(ctx, svc.TemplateID)
			if err != nil {
				return err
			}
			files, err := s.ListTemplateFiles(ctx, tpl.ID)
			if err != nil {
				return err
			}
			for _, attached := range files {
				if attached.Kind == FileKindBinary && attached.Path == file.Path {
					content, err = os.ReadFile(s.templateFilePath(tpl.ID, attached.ID, attached.Kind))
					if err != nil {
						return err
					}
					break
				}
			}
		}
		if err := s.remoteWrite(ctx, srv.Target(), remotePath, content); err != nil {
			return err
		}
	}
	if _, err := s.exec.Exec(ctx, srv.Target(), sshx.CommandSpec{Command: "cd " + shellQuote(svc.RemotePath) + " && docker compose pull && docker compose up -d", Timeout: 2 * time.Minute}); err != nil {
		return err
	}
	return nil
}

func (s *Service) runRemoteLifecycle(ctx context.Context, serviceID, op string) error {
	svc, err := s.GetService(ctx, serviceID)
	if err != nil {
		return err
	}
	srv, err := s.servers.Get(ctx, svc.ServerID)
	if err != nil {
		return err
	}
	command := ""
	switch op {
	case "restart":
		command = "docker compose restart"
	case "stop":
		command = "docker compose stop"
	case "remove":
		command = "docker compose down"
	case "update-images":
		command = "docker compose pull && docker compose up -d"
	default:
		return panelerr.NotFound("compose_operation")
	}
	_, err = s.exec.Exec(ctx, srv.Target(), sshx.CommandSpec{Command: "cd " + shellQuote(svc.RemotePath) + " && " + command, Timeout: 2 * time.Minute})
	return err
}

func (s *Service) remoteMkdir(ctx context.Context, target sshx.Target, path string) error {
	_, err := s.exec.Exec(ctx, target, sshx.CommandSpec{Command: "mkdir -p " + shellQuote(filepath.ToSlash(path)), Timeout: 20 * time.Second})
	return err
}

func (s *Service) remoteWrite(ctx context.Context, target sshx.Target, path string, content []byte) error {
	encoded := base64.StdEncoding.EncodeToString(content)
	command := "printf %s " + shellQuote(encoded) + " | base64 -d > " + shellQuote(filepath.ToSlash(path))
	_, err := s.exec.Exec(ctx, target, sshx.CommandSpec{Command: command, Timeout: 20 * time.Second})
	return err
}

func (s *Service) markApplied(ctx context.Context, serviceID, op string) error {
	svc, err := s.GetService(ctx, serviceID)
	if err != nil {
		return err
	}
	tpl, err := s.GetTemplate(ctx, svc.TemplateID)
	if err != nil {
		return err
	}
	labels := serviceLabels(serviceID, svc.ServerID, svc.TemplateID, tpl.Version)
	raw, _ := json.Marshal(labels)
	_, err = s.db.ExecContext(ctx, `UPDATE deployed_services SET template_version=?,labels_json=?,status=?,drifted=0,updated_at=? WHERE id=?`, tpl.Version, string(raw), ServiceStatusReady, ts(time.Now().UTC()), serviceID)
	return err
}

func (s *Service) bumpTemplateVersion(ctx context.Context, templateID string) error {
	now := ts(time.Now().UTC())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE service_templates SET version=version+1,updated_at=? WHERE id=?`, now, templateID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		_ = tx.Rollback()
		return panelerr.NotFound("service_template")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE deployed_services SET drifted=1,updated_at=? WHERE template_id=?`, now, templateID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Service) persistRendered(ctx context.Context, serviceID string) error {
	svc, err := s.GetService(ctx, serviceID)
	if err != nil {
		return err
	}
	result, err := s.RenderService(ctx, serviceID)
	if err != nil {
		return err
	}
	base := filepath.Join(s.dataRoot, "compose", svc.ServerID, svc.Name, "rendered")
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(base, "compose.yaml"), []byte(result.ComposeYAML), 0600); err != nil {
		return err
	}
	for _, f := range result.Files {
		if f.Kind != FileKindTemplate {
			continue
		}
		path, err := safeJoin(base, f.Path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(f.Content), 0600); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) render(ctx context.Context, tpl ServiceTemplate, req RenderRequest) (RenderResult, error) {
	values, err := s.renderValues(ctx, tpl, req)
	if err != nil {
		return RenderResult{}, err
	}
	composeYAML, err := executeTemplate("compose.yaml", tpl.ComposeYAML, values)
	if err != nil {
		return RenderResult{}, panelerr.Validation("template_render_failed", err.Error())
	}
	files, err := s.ListTemplateFiles(ctx, tpl.ID)
	if err != nil {
		return RenderResult{}, err
	}
	rendered := []RenderedFile{}
	for _, f := range files {
		content, err := os.ReadFile(s.templateFilePath(tpl.ID, f.ID, f.Kind))
		if err != nil {
			return RenderResult{}, err
		}
		if f.Kind == FileKindBinary {
			rendered = append(rendered, RenderedFile{Path: f.Path, Kind: f.Kind, Size: f.Size})
			continue
		}
		out, err := executeTemplate(f.Path, string(content), values)
		if err != nil {
			return RenderResult{}, panelerr.Validation("template_render_failed", err.Error())
		}
		rendered = append(rendered, RenderedFile{Path: f.Path, Kind: f.Kind, Content: out, Size: int64(len(out))})
	}
	return RenderResult{ComposeYAML: composeYAML, Files: rendered, Values: values}, nil
}

func (s *Service) renderValues(ctx context.Context, tpl ServiceTemplate, req RenderRequest) (map[string]any, error) {
	out := map[string]any{}
	for _, v := range tpl.Variables {
		if v.Default != nil {
			out[v.Key] = v.Default
		}
	}
	if req.ServerID != "" {
		vars, err := s.ServerVariables(ctx, req.ServerID)
		if err != nil {
			return nil, err
		}
		for k, v := range vars {
			out[k] = v
		}
	}
	for k, v := range req.Values {
		out[k] = v
	}
	system := map[string]any{
		"server_id":        req.ServerID,
		"service_id":       req.ServiceID,
		"service_name":     req.ServiceName,
		"template_id":      tpl.ID,
		"template_version": tpl.Version,
		"remote_path":      req.RemotePath,
		"rendered_at":      time.Now().UTC().Format(time.RFC3339Nano),
	}
	for k, v := range system {
		out[k] = v
		out["system_"+k] = v
	}
	for _, v := range tpl.Variables {
		if v.Required {
			if _, ok := out[v.Key]; !ok {
				return nil, panelerr.Validation("template_variable_missing", "Missing required template variable: "+v.Key)
			}
		}
	}
	return out, nil
}

func (s *Service) validateServiceRequest(ctx context.Context, req SaveServiceRequest) error {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.ServerID) == "" || strings.TrimSpace(req.TemplateID) == "" || strings.TrimSpace(req.RemotePath) == "" {
		return panelerr.Validation("service_invalid", "Service name, serverId, templateId, and remotePath are required")
	}
	if _, err := cleanRemotePath(req.RemotePath); err != nil {
		return err
	}
	if _, err := s.servers.Get(ctx, req.ServerID); err != nil {
		return err
	}
	_, err := s.GetTemplate(ctx, req.TemplateID)
	return err
}

func validateTemplate(req SaveTemplateRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return panelerr.Validation("service_template_name_required", "Service template name is required")
	}
	if strings.TrimSpace(req.ComposeYAML) == "" {
		return panelerr.Validation("service_template_compose_required", "Compose YAML is required")
	}
	if _, err := parseStrictTemplate("compose.yaml", req.ComposeYAML); err != nil {
		return panelerr.Validation("service_template_invalid", err.Error())
	}
	seen := map[string]bool{}
	for _, v := range req.Variables {
		if strings.TrimSpace(v.Key) == "" {
			return panelerr.Validation("template_variable_invalid", "Template variable key is required")
		}
		if seen[v.Key] {
			return panelerr.Validation("template_variable_duplicate", "Template variable keys must be unique")
		}
		seen[v.Key] = true
	}
	return nil
}

func executeTemplate(name, source string, values map[string]any) (string, error) {
	tpl, err := parseStrictTemplate(name, source)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, values); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func parseStrictTemplate(name, source string) (*template.Template, error) {
	return template.New(name).Option("missingkey=error").Parse(source)
}

func (s *Service) writeTemplateFile(templateID, fileID, kind string, content []byte) error {
	path := s.templateFilePath(templateID, fileID, kind)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0600)
}

func (s *Service) templateFilePath(templateID, fileID, kind string) string {
	dir := "static"
	if kind == FileKindTemplate {
		dir = "templates"
	}
	return filepath.Join(s.dataRoot, "service_templates", templateID, dir, fileID)
}

func (s *Service) attachTemplateFileContent(templateID string, f *TemplateFile) error {
	content, err := os.ReadFile(s.templateFilePath(templateID, f.ID, f.Kind))
	if err != nil {
		return err
	}
	if f.Kind == FileKindTemplate {
		f.Content = string(content)
		return nil
	}
	f.Base64 = base64.StdEncoding.EncodeToString(content)
	return nil
}

func requestContent(req SaveFileRequest, kind string) ([]byte, error) {
	if req.Base64 != "" {
		b, err := base64.StdEncoding.DecodeString(req.Base64)
		if err != nil {
			return nil, panelerr.BadRequest("file_base64_invalid", "Invalid base64 file content")
		}
		return b, nil
	}
	if kind == FileKindBinary && req.Body == "" {
		return nil, panelerr.Validation("file_content_required", "File body or base64 is required")
	}
	return []byte(req.Body), nil
}

func cleanRelativePath(path string) (string, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" || strings.HasPrefix(path, "/") {
		return "", panelerr.Validation("file_path_invalid", "File path must be relative")
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", panelerr.Validation("file_path_invalid", "File path must stay inside the template")
	}
	return clean, nil
}

func cleanRemotePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "\x00") || strings.Contains(path, "..") {
		return "", panelerr.Validation("remote_path_invalid", "Remote path must be an absolute safe path")
	}
	return path, nil
}

func safeJoin(base, rel string) (string, error) {
	clean, err := cleanRelativePath(rel)
	if err != nil {
		return "", err
	}
	out := filepath.Join(base, clean)
	baseAbs, _ := filepath.Abs(base)
	outAbs, _ := filepath.Abs(out)
	if outAbs != baseAbs && !strings.HasPrefix(outAbs, baseAbs+string(filepath.Separator)) {
		return "", fs.ErrPermission
	}
	return out, nil
}

func scanTemplate(row interface{ Scan(dest ...any) error }) (ServiceTemplate, error) {
	var tpl ServiceTemplate
	var visual, vars, created, updated string
	if err := row.Scan(&tpl.ID, &tpl.Name, &tpl.Description, &tpl.ComposeYAML, &visual, &vars, &tpl.Version, &created, &updated); err != nil {
		return ServiceTemplate{}, err
	}
	tpl.VisualState = map[string]any{}
	tpl.Variables = []TemplateVariable{}
	_ = json.Unmarshal([]byte(visual), &tpl.VisualState)
	_ = json.Unmarshal([]byte(vars), &tpl.Variables)
	tpl.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	tpl.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return tpl, nil
}

func scanFile(row interface{ Scan(dest ...any) error }) (TemplateFile, error) {
	var f TemplateFile
	var created, updated string
	if err := row.Scan(&f.ID, &f.TemplateID, &f.Path, &f.Kind, &f.ContentType, &f.Size, &f.SHA256, &created, &updated); err != nil {
		return TemplateFile{}, err
	}
	f.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	f.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return f, nil
}

func scanDeployedService(row interface{ Scan(dest ...any) error }) (DeployedService, error) {
	var svc DeployedService
	var values, labels, created, updated string
	var drifted int
	if err := row.Scan(&svc.ID, &svc.Name, &svc.ServerID, &svc.TemplateID, &svc.TemplateVersion, &svc.RemotePath, &values, &labels, &svc.Status, &drifted, &svc.LastTaskID, &created, &updated); err != nil {
		return DeployedService{}, err
	}
	svc.Values = map[string]any{}
	svc.Labels = map[string]string{}
	_ = json.Unmarshal([]byte(values), &svc.Values)
	_ = json.Unmarshal([]byte(labels), &svc.Labels)
	svc.Drifted = drifted == 1
	svc.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	svc.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return svc, nil
}

func serviceLabels(serviceID, serverID, templateID string, templateVersion int) map[string]string {
	return map[string]string{
		"panel.managed":                  "true",
		"panel.service_template_id":      templateID,
		"panel.service_template_version": strconv.Itoa(templateVersion),
		"panel.service_id":               serviceID,
		"panel.server_id":                serverID,
	}
}

func nonNilMap[M ~map[string]V, V any](m M) M {
	if m == nil {
		return M{}
	}
	return m
}

func checksum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func ts(t time.Time) string { return t.Format(time.RFC3339Nano) }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
