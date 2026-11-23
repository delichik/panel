package sshaccess

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/platform/database/orm"
)

// Repository 读写 security.db 中的 SSH 访问记录。
type Repository struct {
	db  *sql.DB
	now func() time.Time
}

// NewRepository 创建仓储。
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

const sessionColumns = `id,server_id,user_name,tty,pid,source_host,source_address,kind,login_at,logout_at,logout_origin,duration_seconds,last_seen_at,origin`

// UpsertSessions 幂等写入当前在线会话，并返回本次出现的会话 ID（供关闭扫描）。
func (r *Repository) UpsertSessions(ctx context.Context, serverID string, items []agentcontract.SSHSession, observedAt time.Time, origin string) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if observedAt.IsZero() {
		observedAt = r.now()
	}
	observedAt = observedAt.UTC()
	now := r.now().UTC()
	ids := make([]string, 0, len(items))
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, item := range items {
		id := sessionID(serverID, item)
		ids = append(ids, id)
		loginAt := item.LoginAt.UTC()
		if loginAt.IsZero() {
			continue
		}
		if _, err := orm.RawExec(ctx, tx, `INSERT INTO ssh_access_sessions(`+sessionColumns+`,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,NULL,'',NULL,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
 user_name=excluded.user_name,
 tty=excluded.tty,
 pid=excluded.pid,
 source_host=excluded.source_host,
 source_address=excluded.source_address,
 kind=excluded.kind,
 last_seen_at=excluded.last_seen_at,
 updated_at=excluded.updated_at`,
			id, serverID, item.UserName, item.TTY, item.PID, item.SourceHost, item.SourceAddress, item.Kind,
			loginAt.Format(time.RFC3339Nano), observedAt.Format(time.RFC3339Nano), origin,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

// CloseMissingSessions 把本服务器不在 presentIDs 中的开放会话标记为 live 关闭。
func (r *Repository) CloseMissingSessions(ctx context.Context, serverID string, presentIDs []string, at time.Time) (int64, error) {
	if at.IsZero() {
		at = r.now()
	}
	at = at.UTC()
	query := `UPDATE ssh_access_sessions SET
 logout_at=?,
 logout_origin='live',
 duration_seconds=MAX(0, CAST(ROUND((julianday(?) - julianday(login_at)) * 86400) AS INTEGER)),
 updated_at=?
WHERE server_id=? AND logout_at IS NULL`
	args := []any{at.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano), r.now().UTC().Format(time.RFC3339Nano), serverID}
	if len(presentIDs) > 0 {
		query += ` AND id NOT IN (` + placeholders(len(presentIDs)) + `)`
		for _, id := range presentIDs {
			args = append(args, id)
		}
	}
	res, err := orm.RawExec(ctx, r.db, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpsertHistory 用 wtmp 回填补齐登录与登出时间；已有 live 记录只补空缺。
func (r *Repository) UpsertHistory(ctx context.Context, serverID string, items []agentcontract.SSHSessionHistoryItem) error {
	if len(items) == 0 {
		return nil
	}
	now := r.now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, item := range items {
		id := sessionID(serverID, item.SSHSession)
		loginAt := item.LoginAt.UTC()
		if loginAt.IsZero() {
			continue
		}
		var logout any
		var logoutOrigin string
		var duration any
		if !item.LogoutAt.IsZero() {
			logoutAt := item.LogoutAt.UTC()
			seconds := int64(logoutAt.Sub(loginAt) / time.Second)
			if seconds < 0 {
				seconds = 0
			}
			logout = logoutAt.Format(time.RFC3339Nano)
			logoutOrigin = "wtmp"
			duration = seconds
		}
		if _, err := orm.RawExec(ctx, tx, `INSERT INTO ssh_access_sessions(`+sessionColumns+`,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,NULL,'backfill',?,?)
ON CONFLICT(id) DO UPDATE SET
 user_name=excluded.user_name,
 tty=excluded.tty,
 pid=excluded.pid,
 source_host=excluded.source_host,
 source_address=excluded.source_address,
 kind=excluded.kind,
 logout_at=CASE WHEN ssh_access_sessions.logout_at IS NULL AND excluded.logout_at IS NOT NULL THEN excluded.logout_at ELSE ssh_access_sessions.logout_at END,
 logout_origin=CASE WHEN ssh_access_sessions.logout_at IS NULL AND excluded.logout_at IS NOT NULL THEN 'wtmp' ELSE ssh_access_sessions.logout_origin END,
 duration_seconds=CASE WHEN ssh_access_sessions.logout_at IS NULL AND excluded.logout_at IS NOT NULL THEN excluded.duration_seconds ELSE ssh_access_sessions.duration_seconds END,
 updated_at=excluded.updated_at`,
			id, serverID, item.UserName, item.TTY, item.PID, item.SourceHost, item.SourceAddress, item.Kind,
			loginAt.Format(time.RFC3339Nano), logout, logoutOrigin, duration,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// InsertAttempts 逐条写入失败尝试，重复主键直接忽略。
func (r *Repository) InsertAttempts(ctx context.Context, serverID string, items []agentcontract.SSHFailure) (int64, error) {
	if len(items) == 0 {
		return 0, nil
	}
	now := r.now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var inserted int64
	for _, item := range items {
		if item.AttemptAt.IsZero() {
			continue
		}
		id := attemptID(serverID, item)
		res, err := orm.RawExec(ctx, tx, `INSERT INTO ssh_access_attempts(id,server_id,user_name,source_host,source_address,pid,attempt_at,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO NOTHING`,
			id, serverID, item.UserName, item.SourceHost, item.SourceAddress, item.PID,
			item.AttemptAt.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		inserted += n
	}
	return inserted, tx.Commit()
}

// ListSessions 分页读取会话。
func (r *Repository) ListSessions(ctx context.Context, serverID string, f SessionFilter) (SessionPage, error) {
	where, args := sessionWhere(serverID, f)
	page := SessionPage{Items: []Session{}}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ssh_access_sessions`+where, args...).Scan(&total); err != nil {
		return page, err
	}
	page.Total = total
	limit := f.PageSize
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := 0
	if f.Page > 1 {
		offset = (f.Page - 1) * limit
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+sessionColumns+` FROM ssh_access_sessions`+where+
		` ORDER BY login_at DESC, id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanSession(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, toAPISession(item))
	}
	return page, rows.Err()
}

// ListAttempts 分页读取失败尝试。
func (r *Repository) ListAttempts(ctx context.Context, serverID string, f AttemptFilter) (AttemptPage, error) {
	where, args := attemptWhere(serverID, f)
	page := AttemptPage{Items: []Attempt{}}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ssh_access_attempts`+where, args...).Scan(&total); err != nil {
		return page, err
	}
	page.Total = total
	limit := f.PageSize
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := 0
	if f.Page > 1 {
		offset = (f.Page - 1) * limit
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_name,source_host,source_address,pid,attempt_at FROM ssh_access_attempts`+where+
		` ORDER BY attempt_at DESC, id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Attempt
		var pid int64
		var attemptAt string
		if err := rows.Scan(&item.ID, &item.User, &item.SourceHost, &item.SourceAddress, &pid, &attemptAt); err != nil {
			return page, err
		}
		item.PID = int(pid)
		if t, err := time.Parse(time.RFC3339Nano, attemptAt); err == nil {
			item.AttemptAt = t.UTC()
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

// LatestSessionSince 返回该服务器最近一次登录时间，用于增量回填；无记录返回零值。
func (r *Repository) LatestSessionSince(ctx context.Context, serverID string) (time.Time, error) {
	var value sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(login_at) FROM ssh_access_sessions WHERE server_id=?`, serverID).Scan(&value); err != nil {
		return time.Time{}, err
	}
	if !value.Valid || value.String == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return time.Time{}, nil
	}
	return t.UTC(), nil
}

// LatestAttemptSince 返回该服务器最近一次失败尝试时间。
func (r *Repository) LatestAttemptSince(ctx context.Context, serverID string) (time.Time, error) {
	var value sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(attempt_at) FROM ssh_access_attempts WHERE server_id=?`, serverID).Scan(&value); err != nil {
		return time.Time{}, err
	}
	if !value.Valid || value.String == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return time.Time{}, nil
	}
	return t.UTC(), nil
}

// Cleanup 按保留期删除已关闭会话与失败尝试，并限制每服务器失败行数。
func (r *Repository) Cleanup(ctx context.Context, successDays, failureDays, maxRowsPerServer int) (int64, int64, error) {
	now := r.now().UTC()
	if successDays < 1 {
		successDays = 1
	}
	if failureDays < 1 {
		failureDays = 1
	}
	successCutoff := now.Add(-time.Duration(successDays) * 24 * time.Hour).Format(time.RFC3339Nano)
	failureCutoff := now.Add(-time.Duration(failureDays) * 24 * time.Hour).Format(time.RFC3339Nano)
	res, err := orm.RawExec(ctx, r.db, `DELETE FROM ssh_access_sessions WHERE logout_at IS NOT NULL AND logout_at < ?`, successCutoff)
	if err != nil {
		return 0, 0, err
	}
	deletedSessions, _ := res.RowsAffected()
	res, err = orm.RawExec(ctx, r.db, `DELETE FROM ssh_access_attempts WHERE attempt_at < ?`, failureCutoff)
	if err != nil {
		return deletedSessions, 0, err
	}
	deletedAttempts, _ := res.RowsAffected()
	if maxRowsPerServer > 0 {
		overflow, err := r.serversOverAttemptLimit(ctx, maxRowsPerServer)
		if err != nil {
			return deletedSessions, deletedAttempts, err
		}
		for _, serverID := range overflow {
			res, err := orm.RawExec(ctx, r.db, `DELETE FROM ssh_access_attempts WHERE server_id=? AND id NOT IN (
 SELECT id FROM ssh_access_attempts WHERE server_id=? ORDER BY attempt_at DESC, id DESC LIMIT ?)`,
				serverID, serverID, maxRowsPerServer)
			if err != nil {
				return deletedSessions, deletedAttempts, err
			}
			n, _ := res.RowsAffected()
			deletedAttempts += n
		}
	}
	return deletedSessions, deletedAttempts, nil
}

func (r *Repository) serversOverAttemptLimit(ctx context.Context, limit int) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT server_id FROM ssh_access_attempts GROUP BY server_id HAVING COUNT(*) > ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SweepOrphans 删除已不在服务器列表中的访问记录。
func (r *Repository) SweepOrphans(ctx context.Context, serverIDs []string) (int64, error) {
	var deleted int64
	for _, table := range []string{"ssh_access_sessions", "ssh_access_attempts"} {
		query := `DELETE FROM ` + table
		var args []any
		if len(serverIDs) > 0 {
			query += ` WHERE server_id NOT IN (` + placeholders(len(serverIDs)) + `)`
			for _, id := range serverIDs {
				args = append(args, id)
			}
		}
		res, err := orm.RawExec(ctx, r.db, query, args...)
		if err != nil {
			return deleted, err
		}
		n, _ := res.RowsAffected()
		deleted += n
	}
	return deleted, nil
}

func sessionID(serverID string, item agentcontract.SSHSession) string {
	sum := sha256.Sum256([]byte(serverID + "|" + item.UserName + "|" + item.TTY + "|" +
		strconv.Itoa(item.PID) + "|" + strconv.FormatInt(item.LoginAt.UTC().Unix(), 10)))
	return hex.EncodeToString(sum[:])
}

func attemptID(serverID string, item agentcontract.SSHFailure) string {
	sum := sha256.Sum256([]byte(serverID + "|" + item.UserName + "|" + item.SourceHost + "|" +
		item.SourceAddress + "|" + strconv.FormatInt(item.AttemptAt.UTC().Unix(), 10) + "|" + strconv.Itoa(item.PID)))
	return hex.EncodeToString(sum[:])
}

func sessionWhere(serverID string, f SessionFilter) (string, []any) {
	clauses := []string{"server_id=?"}
	args := []any{serverID}
	switch f.State {
	case "open":
		clauses = append(clauses, "logout_at IS NULL")
	case "closed":
		clauses = append(clauses, "logout_at IS NOT NULL")
	}
	if !f.From.IsZero() {
		clauses = append(clauses, "login_at >= ?")
		args = append(args, f.From.UTC().Format(time.RFC3339Nano))
	}
	if !f.To.IsZero() {
		clauses = append(clauses, "login_at <= ?")
		args = append(args, f.To.UTC().Format(time.RFC3339Nano))
	}
	if user := strings.TrimSpace(f.User); user != "" {
		clauses = append(clauses, "user_name = ?")
		args = append(args, user)
	}
	if source := strings.TrimSpace(f.Source); source != "" {
		clauses = append(clauses, "(source_host = ? OR source_address = ?)")
		args = append(args, source, source)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		pattern := "%" + q + "%"
		clauses = append(clauses, "(user_name LIKE ? OR source_host LIKE ? OR source_address LIKE ? OR tty LIKE ?)")
		args = append(args, pattern, pattern, pattern, pattern)
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func attemptWhere(serverID string, f AttemptFilter) (string, []any) {
	clauses := []string{"server_id=?"}
	args := []any{serverID}
	if !f.From.IsZero() {
		clauses = append(clauses, "attempt_at >= ?")
		args = append(args, f.From.UTC().Format(time.RFC3339Nano))
	}
	if !f.To.IsZero() {
		clauses = append(clauses, "attempt_at <= ?")
		args = append(args, f.To.UTC().Format(time.RFC3339Nano))
	}
	if user := strings.TrimSpace(f.User); user != "" {
		clauses = append(clauses, "user_name = ?")
		args = append(args, user)
	}
	if source := strings.TrimSpace(f.Source); source != "" {
		clauses = append(clauses, "(source_host = ? OR source_address = ?)")
		args = append(args, source, source)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		pattern := "%" + q + "%"
		clauses = append(clauses, "(user_name LIKE ? OR source_host LIKE ? OR source_address LIKE ?)")
		args = append(args, pattern, pattern, pattern)
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func scanSession(rows *sql.Rows) (storedSession, error) {
	var item storedSession
	var loginAt string
	var logoutAt, lastSeen sql.NullString
	var duration sql.NullInt64
	var pid int64
	err := rows.Scan(&item.id, &item.serverID, &item.userName, &item.tty, &pid, &item.sourceHost,
		&item.sourceAddress, &item.kind, &loginAt, &logoutAt, &item.logoutOrigin, &duration, &lastSeen, &item.origin)
	if err != nil {
		return item, err
	}
	item.pid = int(pid)
	if t, err := time.Parse(time.RFC3339Nano, loginAt); err == nil {
		item.loginAt = t.UTC()
	}
	if logoutAt.Valid {
		if t, err := time.Parse(time.RFC3339Nano, logoutAt.String); err == nil {
			t = t.UTC()
			item.logoutAt = &t
		}
	}
	if duration.Valid {
		v := duration.Int64
		item.durationSeconds = &v
	}
	if lastSeen.Valid {
		if t, err := time.Parse(time.RFC3339Nano, lastSeen.String); err == nil {
			t = t.UTC()
			item.lastSeenAt = &t
		}
	}
	return item, nil
}

func toAPISession(item storedSession) Session {
	return Session{
		ID: item.id, User: item.userName, TTY: item.tty,
		SourceHost: item.sourceHost, SourceAddress: item.sourceAddress, Kind: item.kind,
		LoginAt: item.loginAt, LogoutAt: item.logoutAt, LogoutOrigin: item.logoutOrigin,
		DurationSeconds: item.durationSeconds, LastSeenAt: item.lastSeenAt,
	}
}
