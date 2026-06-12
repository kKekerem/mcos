package store

import "mcos/internal/model"

// LoadJavaIndex reads the installed-Java registry, empty if absent.
func (s *Store) LoadJavaIndex() (*model.JavaIndex, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ix model.JavaIndex
	if err := readJSON(s.Paths.JavaIndex(), &ix); err != nil {
		if err == ErrNotFound {
			return &model.JavaIndex{}, nil
		}
		return nil, err
	}
	return &ix, nil
}

// SaveJavaIndex persists the installed-Java registry.
func (s *Store) SaveJavaIndex(ix *model.JavaIndex) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.Paths.JavaIndex(), ix)
}

// LoadTunnelRegistry reads the share-code registry, empty if absent.
func (s *Store) LoadTunnelRegistry() (*model.TunnelRegistry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var reg model.TunnelRegistry
	if err := readJSON(s.Paths.TunnelRegistry(), &reg); err != nil {
		if err == ErrNotFound {
			return &model.TunnelRegistry{Entries: map[string]*model.TunnelEntry{}}, nil
		}
		return nil, err
	}
	if reg.Entries == nil {
		reg.Entries = map[string]*model.TunnelEntry{}
	}
	return &reg, nil
}

// SaveTunnelRegistry persists the share-code registry.
func (s *Store) SaveTunnelRegistry(reg *model.TunnelRegistry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.Paths.TunnelRegistry(), reg)
}

// LoadPeers reads the persisted peer list, empty if absent.
func (s *Store) LoadPeers() ([]*model.Peer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var peers []*model.Peer
	if err := readJSON(s.Paths.PeersFile(), &peers); err != nil {
		if err == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return peers, nil
}

// SavePeers persists the peer list.
func (s *Store) SavePeers(peers []*model.Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.Paths.PeersFile(), peers)
}
