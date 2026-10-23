package applications

type EditSessionFileContent struct {
	FileKey       string `json:"-"`
	Name          string `json:"name"`
	Path          string `json:"-"` // Deprecated compatibility alias for name.
	Kind          string `json:"kind"`
	ContentType   string `json:"contentType"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	ContentBase64 string `json:"contentBase64"`
	Content       []byte `json:"-"`
}
