package linux

var distroAdapters = []DistroAdapter{
	DebianAdapter{},
	UbuntuAdapter{},
}

func Supported(info OSRelease) bool {
	_, ok := AdapterFor(info)
	return ok
}

func AdapterFor(info OSRelease) (DistroAdapter, bool) {
	for _, adapter := range distroAdapters {
		if adapter.Supports(info) {
			return adapter, true
		}
	}
	return nil, false
}
