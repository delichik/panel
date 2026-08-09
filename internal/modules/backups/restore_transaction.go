package backups

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"panel/internal/platform/config"
)

const restoreTransactionSchemaVersion = 1

var errSimulatedRestoreCrash = errors.New("simulated restore crash")

type restoreTransactionState struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Phase         string              `json:"phase"`
	Targets       []restoreSwapTarget `json:"targets"`
}

type restoreSwapTarget struct {
	TargetPath      string `json:"targetPath"`
	StagedPath      string `json:"stagedPath"`
	BackupPath      string `json:"backupPath"`
	OriginalExisted bool   `json:"originalExisted"`
	State           string `json:"state"`
}

type restoreRecoveryOutcome struct {
	Found           bool
	Committed       bool
	RolledBack      bool
	RollbackBlocked bool
	Err             error
}

func restoreTransactionDir(dataRoot string) string {
	clean := filepath.Clean(dataRoot)
	return filepath.Join(filepath.Dir(clean), "."+filepath.Base(clean)+"-restore-transaction")
}

func restoreTransactionMediaDir(dataRoot string) string {
	return filepath.Join(restoreTransactionDir(dataRoot), "media")
}

func restoreTransactionStatePath(dataRoot string) string {
	return filepath.Join(restoreTransactionDir(dataRoot), "state.json")
}

func pendingLocation(dataRoot string) string {
	media := restoreTransactionMediaDir(dataRoot)
	if _, err := os.Stat(media); err == nil || !os.IsNotExist(err) {
		return media
	}
	return pendingDir(dataRoot)
}

func ensureRestoreTransactionMedia(dataRoot string) error {
	txnDir := restoreTransactionDir(dataRoot)
	mediaDir := restoreTransactionMediaDir(dataRoot)
	if _, err := os.Stat(filepath.Join(mediaDir, "pending.json")); err == nil {
		return securePendingPermissions(mediaDir)
	}
	if err := os.MkdirAll(txnDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(txnDir, 0700); err != nil {
		return err
	}
	source := pendingDir(dataRoot)
	sourceParent := filepath.Dir(source)
	if err := securePendingPermissions(source); err != nil {
		return err
	}
	if err := os.Rename(source, mediaDir); err != nil {
		return fmt.Errorf("move restore media outside replacement range: %w", err)
	}
	if err := syncDirectory(txnDir); err != nil {
		return err
	}
	if err := syncDirectory(sourceParent); err != nil {
		return err
	}
	return securePendingPermissions(mediaDir)
}

func prepareRestoreTransaction(cfg config.Config, extracted string) (restoreTransactionState, error) {
	txnDir := restoreTransactionDir(cfg.DataRoot)
	workDir := filepath.Join(txnDir, "work")
	if err := os.RemoveAll(workDir); err != nil {
		return restoreTransactionState{}, err
	}
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return restoreTransactionState{}, err
	}
	stagedRoot := filepath.Join(workDir, "data-root")
	if err := copyDir(filepath.Join(extracted, "dataRoot"), stagedRoot); err != nil {
		return restoreTransactionState{}, err
	}
	state := restoreTransactionState{SchemaVersion: restoreTransactionSchemaVersion, Phase: "prepared"}
	var externalStages []string
	prepared := false
	defer func() {
		if !prepared {
			for _, path := range externalStages {
				_ = os.RemoveAll(path)
			}
		}
	}()
	state.Targets = append(state.Targets, restoreSwapTarget{
		TargetPath: cfg.DataRoot, StagedPath: stagedRoot,
		BackupPath: filepath.Join(txnDir, "backup", "data-root"), State: "pending",
	})
	for i, item := range []struct {
		sources []string
		target  string
	}{
		{[]string{filepath.Join(extracted, "databases", "app.db")}, cfg.AppDatabase},
		{[]string{filepath.Join(extracted, "databases", "log.db"), filepath.Join(extracted, "databases", "tasks.db")}, cfg.LogDatabase},
		{[]string{filepath.Join(extracted, "databases", "metrics.db")}, cfg.MetricsDatabase},
		{[]string{filepath.Join(extracted, "databases", "coordination.db")}, cfg.CoordinationDatabase},
	} {
		source := firstExistingPath(item.sources)
		if source == "" || item.target == "" {
			continue
		}
		if rel, ok := pathWithin(cfg.DataRoot, item.target); ok {
			if err := copyFile(source, filepath.Join(stagedRoot, rel), 0600); err != nil {
				return restoreTransactionState{}, err
			}
			continue
		}
		staged := filepath.Join(filepath.Dir(item.target), fmt.Sprintf(".%s.panel-restore-stage-%d", filepath.Base(item.target), i))
		backup := filepath.Join(filepath.Dir(item.target), fmt.Sprintf(".%s.panel-restore-backup-%d", filepath.Base(item.target), i))
		if pathExists(backup) {
			return restoreTransactionState{}, fmt.Errorf("external restore backup already exists: %s", backup)
		}
		_ = os.RemoveAll(staged)
		if err := copyFile(source, staged, 0600); err != nil {
			return restoreTransactionState{}, err
		}
		externalStages = append(externalStages, staged)
		if err := syncFile(staged); err != nil {
			return restoreTransactionState{}, err
		}
		if err := syncDirectory(filepath.Dir(staged)); err != nil {
			return restoreTransactionState{}, err
		}
		state.Targets = append(state.Targets, restoreSwapTarget{
			TargetPath: item.target, StagedPath: staged,
			BackupPath: backup, State: "pending",
		})
	}
	if err := syncRestoreTree(workDir); err != nil {
		return restoreTransactionState{}, err
	}
	if err := writeRestoreTransactionState(cfg.DataRoot, state); err != nil {
		return restoreTransactionState{}, err
	}
	prepared = true
	return state, nil
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func syncRestoreTree(root string) error {
	var dirs []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		err = file.Sync()
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	}); err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := syncDirectory(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}

func applyRestoreTransaction(dataRoot string, state restoreTransactionState, hook func(string) error) error {
	state.Phase = "applying"
	if err := writeRestoreTransactionState(dataRoot, state); err != nil {
		return err
	}
	for i := range state.Targets {
		target := &state.Targets[i]
		if hook != nil {
			if err := hook(fmt.Sprintf("before_swap_%d", i)); err != nil {
				return handleRestoreApplyError(dataRoot, state, err)
			}
		}
		if _, err := os.Stat(target.TargetPath); err == nil {
			target.OriginalExisted = true
			if err := os.MkdirAll(filepath.Dir(target.BackupPath), 0700); err != nil {
				return handleRestoreApplyError(dataRoot, state, err)
			}
			_ = os.RemoveAll(target.BackupPath)
			target.State = "backup_planned"
			if err := writeRestoreTransactionState(dataRoot, state); err != nil {
				return handleRestoreApplyError(dataRoot, state, err)
			}
			if err := os.Rename(target.TargetPath, target.BackupPath); err != nil {
				return handleRestoreApplyError(dataRoot, state, err)
			}
			if err := syncDirectory(filepath.Dir(target.TargetPath)); err != nil {
				return handleRestoreApplyError(dataRoot, state, err)
			}
			if err := syncDirectory(filepath.Dir(target.BackupPath)); err != nil {
				return handleRestoreApplyError(dataRoot, state, err)
			}
		}
		target.State = "backup_moved"
		if err := writeRestoreTransactionState(dataRoot, state); err != nil {
			return handleRestoreApplyError(dataRoot, state, err)
		}
		if err := os.MkdirAll(filepath.Dir(target.TargetPath), 0700); err != nil {
			return handleRestoreApplyError(dataRoot, state, err)
		}
		if err := os.Rename(target.StagedPath, target.TargetPath); err != nil {
			return handleRestoreApplyError(dataRoot, state, err)
		}
		if err := syncDirectory(filepath.Dir(target.TargetPath)); err != nil {
			return handleRestoreApplyError(dataRoot, state, err)
		}
		target.State = "swapped"
		if err := writeRestoreTransactionState(dataRoot, state); err != nil {
			return handleRestoreApplyError(dataRoot, state, err)
		}
		if hook != nil {
			if err := hook(fmt.Sprintf("after_swap_%d", i)); err != nil {
				return handleRestoreApplyError(dataRoot, state, err)
			}
		}
	}
	state.Phase = "committed"
	return writeRestoreTransactionState(dataRoot, state)
}

func handleRestoreApplyError(dataRoot string, state restoreTransactionState, applyErr error) error {
	if errors.Is(applyErr, errSimulatedRestoreCrash) {
		return applyErr
	}
	if rollbackErr := rollbackRestoreTransaction(dataRoot, state); rollbackErr != nil {
		return fmt.Errorf("restore apply failed: %v; rollback failed: %w", applyErr, rollbackErr)
	}
	return applyErr
}

func rollbackRestoreTransaction(dataRoot string, state restoreTransactionState) error {
	state.Phase = "rolling_back"
	if err := writeRestoreTransactionState(dataRoot, state); err != nil {
		return err
	}
	for i := len(state.Targets) - 1; i >= 0; i-- {
		target := &state.Targets[i]
		backupExists := pathExists(target.BackupPath)
		if target.State == "rollback_renaming" {
			if !backupExists && pathExists(target.TargetPath) {
				target.State = "rolled_back"
				if err := writeRestoreTransactionState(dataRoot, state); err != nil {
					return err
				}
				continue
			}
			if !backupExists {
				return fmt.Errorf("restore rollback target and backup are missing for %s", target.TargetPath)
			}
		}
		if target.OriginalExisted && (target.State == "backup_moved" || target.State == "swapped") && !backupExists {
			return fmt.Errorf("restore rollback backup is missing for %s", target.TargetPath)
		}
		switch target.State {
		case "swapped":
			if err := os.RemoveAll(target.TargetPath); err != nil {
				return err
			}
		case "backup_moved":
			if pathExists(target.TargetPath) && backupExists {
				if err := os.RemoveAll(target.TargetPath); err != nil {
					return err
				}
			} else if pathExists(target.TargetPath) && !target.OriginalExisted {
				if err := os.RemoveAll(target.TargetPath); err != nil {
					return err
				}
			}
		case "pending":
			if !backupExists {
				continue
			}
		case "backup_planned":
			if !backupExists {
				if !pathExists(target.TargetPath) {
					return fmt.Errorf("restore original target disappeared before backup: %s", target.TargetPath)
				}
				target.State = "rolled_back"
				if err := writeRestoreTransactionState(dataRoot, state); err != nil {
					return err
				}
				continue
			}
		}
		if backupExists {
			if err := os.MkdirAll(filepath.Dir(target.TargetPath), 0700); err != nil {
				return err
			}
			target.State = "rollback_renaming"
			if err := writeRestoreTransactionState(dataRoot, state); err != nil {
				return err
			}
			if err := os.Rename(target.BackupPath, target.TargetPath); err != nil {
				return err
			}
			if err := syncDirectory(filepath.Dir(target.TargetPath)); err != nil {
				return err
			}
		}
		target.State = "rolled_back"
		if err := writeRestoreTransactionState(dataRoot, state); err != nil {
			return err
		}
	}
	state.Phase = "rolled_back"
	return writeRestoreTransactionState(dataRoot, state)
}

func recoverRestoreTransaction(dataRoot string) restoreRecoveryOutcome {
	state, err := readRestoreTransactionState(dataRoot)
	if os.IsNotExist(err) {
		return restoreRecoveryOutcome{}
	}
	if err != nil {
		return restoreRecoveryOutcome{Found: true, RollbackBlocked: true, Err: err}
	}
	if state.Phase == "committed" {
		return restoreRecoveryOutcome{Found: true, Committed: true}
	}
	if state.Phase == "rolled_back" {
		return restoreRecoveryOutcome{Found: true, RolledBack: true}
	}
	if err := rollbackRestoreTransaction(dataRoot, state); err != nil {
		return restoreRecoveryOutcome{Found: true, RollbackBlocked: true, Err: err}
	}
	return restoreRecoveryOutcome{Found: true, RolledBack: true}
}

func cleanupRestoreTransaction(dataRoot string) error {
	state, err := readRestoreTransactionState(dataRoot)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		for _, target := range state.Targets {
			if target.StagedPath != "" && pathExists(target.StagedPath) {
				if err := os.RemoveAll(target.StagedPath); err != nil {
					return err
				}
			}
			if target.BackupPath != "" && pathExists(target.BackupPath) {
				if state.Phase != "committed" && state.Phase != "rolled_back" {
					return errors.New("restore transaction cleanup requested before resolution")
				}
				if err := os.RemoveAll(target.BackupPath); err != nil {
					return err
				}
			}
		}
	}
	return os.RemoveAll(restoreTransactionDir(dataRoot))
}

func readRestoreTransactionState(dataRoot string) (restoreTransactionState, error) {
	raw, err := os.ReadFile(restoreTransactionStatePath(dataRoot))
	if err != nil {
		return restoreTransactionState{}, err
	}
	var state restoreTransactionState
	if err := json.Unmarshal(raw, &state); err != nil {
		return restoreTransactionState{}, err
	}
	if state.SchemaVersion != restoreTransactionSchemaVersion {
		return restoreTransactionState{}, errors.New("unsupported restore transaction state")
	}
	return state, nil
}

func writeRestoreTransactionState(dataRoot string, state restoreTransactionState) error {
	path := restoreTransactionStatePath(dataRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		_ = os.Remove(path + ".tmp")
		tmp, err = os.OpenFile(path+".tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func pathWithin(root, target string) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func firstExistingPath(paths []string) string {
	for _, path := range paths {
		if pathExists(path) {
			return path
		}
	}
	return ""
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
