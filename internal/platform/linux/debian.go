package linux

type DebianAdapter struct {
	aptAdapter
}

func (DebianAdapter) ID() string { return "debian" }

func (DebianAdapter) Supports(info OSRelease) bool {
	return info.ID == "debian" && (info.VersionID == "12" || info.VersionID == "13")
}
