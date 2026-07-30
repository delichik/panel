package applications

type EditSessionFileContent struct {
	FileKey       string `json:"fileKey"`
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	ContentType   string `json:"contentType"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	ContentBase64 string `json:"contentBase64"`
	Content       []byte `json:"-"`
}
