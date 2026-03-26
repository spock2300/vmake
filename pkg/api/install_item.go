package api

type InstallItemHolder struct {
	items  []InstallItem
	filter InstallFilterFunc
}

func (h *InstallItemHolder) addInstall(src, dest string) {
	h.items = append(h.items, InstallItem{Src: src, Dest: dest})
}

func (h *InstallItemHolder) getInstallItems() []InstallItem {
	return h.items
}

func (h *InstallItemHolder) setInstallFilter(filter InstallFilterFunc) {
	h.filter = filter
}

func (h *InstallItemHolder) getInstallFilter() InstallFilterFunc {
	return h.filter
}
