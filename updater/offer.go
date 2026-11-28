package updater

import (
	"errors"
	"sync"
)

// Offer is an available update: the version it was published for and the download URL that goes
// with it. The two always travel together.
type Offer struct{ Version, URL string }

var (
	ErrNoOffer    = errors.New("no update available")
	ErrStaleOffer = errors.New("the offered update changed")
	ErrNoVersion  = errors.New("an update version is required")
)

// OfferStore holds the single current offer. Checks take a sequence number when they start and
// publish compare-and-set: a check that started earlier never replaces the result of one that
// started later, however the two finish.
type OfferStore struct {
	mu        sync.Mutex
	seq       uint64
	published uint64
	cur       Offer
}

// Begin registers a starting check and returns its sequence number.
func (s *OfferStore) Begin() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return s.seq
}

// Publish stores o (the zero Offer means "up to date") unless a later check already published.
func (s *OfferStore) Publish(seq uint64, o Offer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq <= s.published {
		return false
	}
	s.published, s.cur = seq, o
	return true
}

// Current returns the current offer (zero when none).
func (s *OfferStore) Current() Offer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// Take returns the offer to install. expect is required and must equal the current version, so an
// Install button cannot install a different release than the one it displayed.
func (s *OfferStore) Take(expect string) (Offer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expect == "" {
		return Offer{}, ErrNoVersion
	}
	if s.cur.URL == "" {
		return Offer{}, ErrNoOffer
	}
	if s.cur.Version != expect {
		return Offer{}, ErrStaleOffer
	}
	return s.cur, nil
}

// Clear drops the offer only if version (required; empty is ignored) is still the current one, so
// a stale prompt's Skip cannot remove a newer offer.
func (s *OfferStore) Clear(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version != "" && s.cur.Version == version {
		s.cur = Offer{}
	}
}
