package facilityapps

import (
	"archive/zip"
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

type facilityAssetDownloadReader interface {
	GetFacilityEditAssetDownload(context.Context, string, string) (FacilityAssetDownload, error)
	GetStaticAssetDownload(context.Context, string) (FacilityAssetDownload, error)
}

func (h *Handler) DownloadFacilityEditAsset(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(facilityAssetDownloadReader)
	if !ok {
		httpx.Error(w, panelerr.New(http.StatusNotImplemented, "facility_asset_download_unavailable", "Facility asset downloads are not available"))
		return
	}
	result, err := service.GetFacilityEditAssetDownload(r.Context(), r.PathValue("id"), r.PathValue("assetName"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	serveFacilityAssetDownload(w, r, result)
}

func (h *Handler) DownloadStaticAsset(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(facilityAssetDownloadReader)
	if !ok {
		httpx.Error(w, panelerr.New(http.StatusNotImplemented, "facility_asset_download_unavailable", "Facility asset downloads are not available"))
		return
	}
	result, err := service.GetStaticAssetDownload(r.Context(), r.PathValue("assetName"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	serveFacilityAssetDownload(w, r, result)
}

func serveFacilityAssetDownload(w http.ResponseWriter, r *http.Request, asset FacilityAssetDownload) {
	if asset.Kind == StaticSourceUploadedFile {
		filename := safeAssetFilename(asset.Filename)
		pathValue := filepath.Join(asset.Root, filename)
		file, err := os.Open(pathValue)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			if err == nil {
				err = panelerr.Validation("facility_static_asset_content_invalid", "static asset content is invalid")
			}
			httpx.Error(w, err)
			return
		}
		contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
		if asset.ContentMode == "text" && contentType == "" {
			contentType = "text/plain; charset=utf-8"
		}
		if contentType == "" {
			var sniff [512]byte
			n, _ := file.Read(sniff[:])
			contentType = http.DetectContentType(sniff[:n])
			_, _ = file.Seek(0, io.SeekStart)
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", contentDisposition(filename))
		http.ServeContent(w, r, filename, info.ModTime(), file)
		return
	}

	files, err := facilityBundleFiles(asset.Root)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	// 先预打开全部文件，任何读取失败都在写响应头之前以错误响应返回，
	// 避免“下载出错仍返回 200”的半成品 zip。
	handles := make([]*os.File, 0, len(files))
	for _, filePath := range files {
		file, openErr := os.Open(filePath)
		if openErr != nil {
			for _, opened := range handles {
				_ = opened.Close()
			}
			httpx.Error(w, openErr)
			return
		}
		handles = append(handles, file)
	}
	defer func() {
		for _, file := range handles {
			_ = file.Close()
		}
	}()
	filename := facilityBundleDownloadName(asset.Filename)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition(filename))
	zw := zip.NewWriter(w)
	for i, filePath := range files {
		rel, _ := filepath.Rel(asset.Root, filePath)
		entry, createErr := zw.Create(filepath.ToSlash(rel))
		if createErr != nil {
			logging.L().Warn("facility asset zip entry create failed", zap.String("file", filePath), zap.Error(createErr))
			return
		}
		if _, copyErr := io.Copy(entry, handles[i]); copyErr != nil {
			logging.L().Warn("facility asset zip copy failed", zap.String("file", filePath), zap.Error(copyErr))
			return
		}
	}
	if err := zw.Close(); err != nil {
		logging.L().Warn("facility asset zip close failed", zap.String("root", asset.Root), zap.Error(err))
	}
}

func facilityBundleFiles(root string) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(pathValue string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return panelerr.Validation("facility_static_asset_content_invalid", "static asset content is invalid")
		}
		files = append(files, pathValue)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, panelerr.Validation("facility_static_asset_archive_empty", "Static asset archive is empty")
	}
	return files, nil
}

func facilityBundleDownloadName(filename string) string {
	name := safeAssetFilename(filename)
	lower := strings.ToLower(name)
	for _, suffix := range []string{".tar.gz", ".tgz", ".tar", ".zip"} {
		if strings.HasSuffix(lower, suffix) {
			name = name[:len(name)-len(suffix)]
			break
		}
	}
	if strings.TrimSpace(name) == "" {
		name = "facility-asset"
	}
	return name + ".zip"
}

func contentDisposition(filename string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": filename})
}
