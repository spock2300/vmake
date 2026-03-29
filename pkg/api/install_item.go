package api

type InstallItemHolder struct {
	items  []InstallItem
	filter InstallFilterFunc
}

func (h *InstallItemHolder) AddInstalls(src, dest string) *InstallItemHolder {
	h.items = append(h.items, InstallItem{Src: src, Dest: dest})
	return h
}

func (h *InstallItemHolder) GetInstallItems() []InstallItem {
	return h.items
}

func (h *InstallItemHolder) SetInstallFilter(filter InstallFilterFunc) *InstallItemHolder {
	h.filter = filter
	return h
}

func (h *InstallItemHolder) GetInstallFilter() InstallFilterFunc {
	return h.filter
}
