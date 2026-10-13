package accountsdb

import (
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"testing"
)

func TestProgramCacheSourceBinding(t *testing.T) {
	f := features.NewFeaturesDefault()
	source := []byte{1, 2, 3}
	entry := &ProgramCacheEntry{DeploymentSlot: 100}
	if entry.MatchesSource(source, f) {
		t.Fatal("unbound entry matched")
	}
	entry.BindSource(source, f)
	if !entry.MatchesSource(source, f) {
		t.Fatal("identical source missed")
	}
	source[0]++
	if entry.MatchesSource(source, f) {
		t.Fatal("same-slot fork source matched")
	}
	source[0]--
	f.EnableFeature(features.VirtualAddressSpaceAdjustments, 0)
	if entry.MatchesSource(source, f) {
		t.Fatal("different feature environment matched")
	}
	if !entry.MatchesSource(source, features.NewFeaturesDefault()) {
		t.Fatal("binding retained mutable feature map")
	}
	entry.BindImmutableSource(source, f)
	if !entry.MatchesSource(source, f) {
		t.Fatal("private source missed")
	}
	if entry.MatchesSource(append([]byte(nil), source...), f) {
		t.Fatal("replacement retained private binding")
	}
	f.EnableFeature(features.VirtualAddressSpaceAdjustments, 1)
	if entry.MatchesSource(source, f) {
		t.Fatal("private binding ignored changed features")
	}
	entry.BindSource(source, f)
	source[0]++
	if entry.MatchesSource(source, f) {
		t.Fatal("ordinary rebind retained private trust")
	}
}
