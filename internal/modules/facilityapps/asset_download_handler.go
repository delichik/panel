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
	result, err := service.GetFacilityEditAssetDownload(r.Context(), r.PathValue("id"), r.PathValue("assetKey"))
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
	result, err := service.GetStaticAssetDownload(r.Context(), r.PathValue("assetId"))
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
	filename := facilityBundleDownloadName(asset.Filename)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition(filename))
	zw := zip.NewWriter(w)
	for _, filePath := range files {
		rel, _ := filepath.Rel(asset.Root, filePath)
		entry, createErr := zw.Create(filepath.ToSlash(rel))
		if createErr != nil {
			break
		}
		file, openErr := os.Open(filePath)
		if openErr != nil {
			break
		}
		_, copyErr := io.Copy(entry, file)
		_ = file.Close()
		if copyErr != nil {
			break
		}
	}
	_ = zw.Close()
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
