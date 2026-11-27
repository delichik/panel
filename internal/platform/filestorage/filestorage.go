// Package filestorage provides shared on-disk helpers for application files and
// facility static assets (atomic writes, directory hashes, workspace staleness).
package filestorage

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WriteAtomic writes content to path via a sibling .partial file then rename.
func WriteAtomic(path string, content []byte, perm os.FileMode) error {
	if perm == 0 {
		perm = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	partial := path + ".partial"
	if err := os.WriteFile(partial, content, perm); err != nil {
		return err
	}
	if err := os.Rename(partial, path); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return nil
}

// CopyAtomic copies src to dst via a sibling .partial file then rename.
func CopyAtomic(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	partial := dst + ".partial"
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(partial)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(partial)
		return closeErr
	}
	if err := os.Rename(partial, dst); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return nil
}

// RemoveQuiet removes path when non-empty, ignoring errors.
func RemoveQuiet(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

// RemoveAllQuiet removes a path tree when non-empty, ignoring errors.
func RemoveAllQuiet(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	_ = os.RemoveAll(path)
}

// SHA256Hex returns the hex-encoded SHA-256 of content.
func SHA256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// SafeFilename returns a path-safe base filename or empty when invalid.
func SafeFilename(value string) string {
	value = strings.TrimSpace(filepath.Base(strings.ReplaceAll(value, "\\", "/")))
	value = strings.Trim(value, ". ")
	if value == "" || strings.ContainsAny(value, `/\`) {
		return ""
	}
	return value
}

// HashDirectory returns a stable SHA-256 over relative paths, sizes, and bytes
// of every regular file under root (facility/application content trees).
func HashDirectory(root string) (string, error) {
	hasher := sha256.New()
	var entryCount uint64
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relBytes := []byte(filepath.ToSlash(rel))
		if err := binary.Write(hasher, binary.BigEndian, uint64(len(relBytes))); err != nil {
			return err
		}
		if _, err := hasher.Write(relBytes); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := binary.Write(hasher, binary.BigEndian, uint64(info.Size())); err != nil {
			return err
		}
		file, err := os.Open(current)
		if err != nil {
			return err
		}
		copied, copyErr := io.Copy(hasher, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if copied != info.Size() {
			return io.ErrUnexpectedEOF
		}
		if _, err := hasher.Write([]byte{0xff}); err != nil {
			return err
		}
		entryCount++
		return nil
	})
	if err != nil {
		return "", err
	}
	if err := binary.Write(hasher, binary.BigEndian, entryCount); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// WorkspaceStale reports whether dir and all nested entries are older than age
// relative to now (used by edit-session orphan cleanup).
func WorkspaceStale(dir string, now time.Time, age time.Duration) bool {
	newest, err := os.Stat(dir)
	if err != nil {
		return false
	}
	latest := newest.ModTime()
	_ = filepath.WalkDir(dir, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info, infoErr := entry.Info(); infoErr == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	return now.Sub(latest) >= age
}
