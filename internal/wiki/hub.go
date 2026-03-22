package wiki

import "sort"

// Hub maps wiki names to Wiki instances and provides lookup.
type Hub struct {
	wikis map[string]*Wiki
}

// NewHub creates a Hub with named wikis.
func NewHub(wikis map[string]*Wiki) *Hub {
	return &Hub{wikis: wikis}
}

// Wiki returns the named wiki, or nil.
func (h *Hub) Wiki(name string) *Wiki {
	return h.wikis[name]
}

// Names returns sorted wiki names.
func (h *Hub) Names() []string {
	names := make([]string, 0, len(h.wikis))
	for name := range h.wikis {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
