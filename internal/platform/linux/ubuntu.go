package linux

type UbuntuAdapter struct {
	aptAdapter
}

func (UbuntuAdapter) ID() string { return "ubuntu" }

func (UbuntuAdapter) Supports(info OSRelease) bool {
	if info.ID != "ubuntu" {
		return false
	}

	switch info.VersionID {
	case "20.04", "22.04", "24.04", "24.10", "25.04", "25.10", "26.04":
		return true
	default:
		return false
	}
}
