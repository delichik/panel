package applications

import (
	"bytes"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

func inferApplicationFileContentType(name string, content []byte, template bool) string {
	if value := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); value != "" {
		return value
	}
	if template {
		return "text/plain; charset=utf-8"
	}
	return http.DetectContentType(content)
}

func serveApplicationFileContent(w http.ResponseWriter, r *http.Request, name, contentType string, content []byte) {
	name = safeDownloadName(name)
	if strings.TrimSpace(contentType) == "" {
		contentType = http.DetectContentType(content)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", applicationContentDisposition(name))
	http.ServeContent(w, r, name, timeZero, bytes.NewReader(content))
}

var timeZero time.Time

func applicationContentDisposition(name string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": safeDownloadName(name)})
}

func safeDownloadName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r == '"' || r == '/' || r == '\\' || r == 0 || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" {
		return "application-package.zip"
	}
	return name
}
