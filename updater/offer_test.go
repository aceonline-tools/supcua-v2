package updater

import (
	"errors"
	"testing"
)

var (
	offerA = Offer{"1.0.1", "https://github.com/x/releases/download/v1.0.1/supcua-v2.exe"}
	offerB = Offer{"1.0.2", "https://github.com/x/releases/download/v1.0.2/supcua-v2.exe"}
)

// Interleaving 1: an older check (startup, A) resumes after a newer one (manual, B) published.
func TestOfferOlderCheckCannotOverwriteNewer(t *testing.T) {
	var s OfferStore
	startup := s.Begin()
	manual := s.Begin()
	if !s.Publish(manual, offerB) {
		t.Fatal("newer publish refused")
	}
	if s.Publish(startup, offerA) {
		t.Fatal("older check overwrote the newer result")
	}
	if got := s.Current(); got != offerB {
		t.Fatalf("current = %+v, want B", got)
	}
	if o, err := s.Take(offerB.Version); err != nil || o != offerB {
		t.Fatalf("take B = %+v %v", o, err)
	}
}

func TestOfferOlderPublishFirstThenNewerWins(t *testing.T) {
	var s OfferStore
	startup := s.Begin()
	manual := s.Begin()
	s.Publish(startup, offerA)
	s.Publish(manual, offerB)
	if s.Current() != offerB {
		t.Fatalf("current = %+v", s.Current())
	}
}

func TestOfferNewerUpToDateClearsOlderOffer(t *testing.T) {
	var s OfferStore
	a, b := s.Begin(), s.Begin()
	s.Publish(a, offerA)
	s.Publish(b, Offer{})
	if s.Current() != (Offer{}) {
		t.Fatalf("current = %+v", s.Current())
	}
}

// Interleaving 2: a stale startup prompt for A after a manual check published B.
func TestOfferStalePromptCannotSkipOrInstallNewer(t *testing.T) {
	var s OfferStore
	startup := s.Begin()
	manual := s.Begin()
	s.Publish(manual, offerB)
	if s.Publish(startup, offerA) {
		t.Fatal("stale startup published")
	}
	s.Clear(offerA.Version) // the stale dialog's Skip
	if s.Current() != offerB {
		t.Fatalf("stale Skip cleared the newer offer: %+v", s.Current())
	}
	if _, err := s.Take(offerA.Version); !errors.Is(err, ErrStaleOffer) {
		t.Fatalf("install of the stale version: %v", err)
	}
	s.Clear(offerB.Version)
	if _, err := s.Take(""); !errors.Is(err, ErrNoOffer) {
		t.Fatalf("after skipping B: %v", err)
	}
}

func TestOfferClearWithoutVersionIsUnconditional(t *testing.T) {
	var s OfferStore
	s.Publish(s.Begin(), offerA)
	s.Clear("")
	if s.Current() != (Offer{}) {
		t.Fatal("not cleared")
	}
}
