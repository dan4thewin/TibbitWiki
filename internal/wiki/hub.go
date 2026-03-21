package wiki

import "sort"

// Hub maps wiki names to Wiki instances and provides lookup.
type Hub struct {
	wikis map[string]*Wiki
	def   *Wiki
}

// NewSingleHub creates a Hub with a single default wiki.
func NewSingleHub(w *Wiki) *Hub {
	return &Hub{def: w}
}

// NewMultiHub creates a Hub with named wikis.
func NewMultiHub(wikis map[string]*Wiki) *Hub {
	return &Hub{wikis: wikis}
}

// Default returns the default wiki (single-wiki mode), or nil.
func (h *Hub) Default() *Wiki { return h.def }

// Wiki returns the named wiki, or nil.
func (h *Hub) Wiki(name string) *Wiki {
	if h.wikis == nil {
		return nil
	}
	return h.wikis[name]
}

// Names returns sorted wiki names (multi-wiki mode).
func (h *Hub) Names() []string {
	if h.wikis == nil {
		return nil
	}
	names := make([]string, 0, len(h.wikis))
	for name := range h.wikis {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsMulti returns true if this hub serves multiple wikis.
func (h *Hub) IsMulti() bool { return h.def == nil }
