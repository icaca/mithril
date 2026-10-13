package sealevel

import (
	"bytes"
	"testing"

	"github.com/sonicfromnewyoke/mithril/fixtures"
	"github.com/sonicfromnewyoke/mithril/pkg/accountsdb"
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/stretchr/testify/require"
)

func TestSharedProgramContentAndFeatures(t *testing.T) {
	source := append(fixtures.Load(t, "sealevel", "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr.so"), 0)
	f := features.NewFeaturesDefault()
	p, err := LoadSharedProgram(source, f, true)
	require.NoError(t, err)
	again, err := LoadSharedProgram(bytes.Clone(source), features.NewFeaturesDefault(), false)
	require.NoError(t, err)
	require.Same(t, p, again, "a deployment-verified program must also serve execution")

	ctx := &ExecutionCtx{Features: *f, SlotCtx: &SlotCtx{AccountsDb: &accountsdb.AccountsDb{}}}
	uncached, err := deployProgram(ctx, source)
	require.NoError(t, err)
	require.NotSame(t, p, uncached, "ordinary banks retain their bounded program-cache lifetime")
	ctx.SlotCtx.AccountsDb.UseSharedProgramCache = true
	shared, err := deployProgram(ctx, source)
	require.NoError(t, err)
	require.Same(t, p, shared, "only opted-in VMs share deployment validation")

	source[len(source)-1] = 1 // trailing capacity, same length and deployment slot
	changed, err := LoadSharedProgram(source, f, false)
	require.NoError(t, err)
	require.NotSame(t, p, changed, "same-length source change must miss")
	strict, err := LoadSharedProgram(source, f, true)
	require.NoError(t, err)
	require.Same(t, changed, strict, "runtime-first load must not require another parse for deployment")
	source[len(source)-1] = 0
	f.EnableFeature(features.FeatureGate{Name: "shared-program-test", Address: [32]byte{178}}, 1)
	changed, err = LoadSharedProgram(source, f, true)
	require.NoError(t, err)
	require.NotSame(t, p, changed, "different features must miss")
	f.EnableFeature(features.FeatureGate{Name: "shared-program-test", Address: [32]byte{178}}, 2)
	another, err := LoadSharedProgram(source, f, true)
	require.NoError(t, err)
	require.NotSame(t, changed, another, "activation slot is part of feature identity")

	// Runtime permits unresolved syscalls; deployment must still reject them.
	invalid := bytes.Replace(source, []byte("sol_log_"), []byte("bad_log_"), 1)
	require.NotEqual(t, source, invalid)
	_, err = LoadSharedProgram(invalid, f, false)
	require.NoError(t, err)
	_, err = LoadSharedProgram(invalid, f, true)
	require.Error(t, err)
}

func TestSharedProgramHashCollisions(t *testing.T) {
	source := append(fixtures.Load(t, "sealevel", "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr.so"), 0)
	f := features.NewFeaturesDefault()
	// Supply one hash for every source to exercise the actual collision bucket.
	const hash = 0
	first, err := loadSharedProgram(source, f, true, hash)
	require.NoError(t, err)
	original := bytes.Clone(source)
	source[len(source)-1] = 1
	second, err := loadSharedProgram(source, f, true, hash)
	require.NoError(t, err)
	require.NotSame(t, first, second, "colliding same-length sources must compile independently")

	again, err := loadSharedProgram(original, f, false, hash)
	require.NoError(t, err)
	require.Same(t, first, again, "the cache must own a source copy and retain both colliding entries")
	again, err = loadSharedProgram(bytes.Clone(source), f, false, hash)
	require.NoError(t, err)
	require.Same(t, second, again)

	source[0] = 0 // invalid ELF, still the same length and forced hash
	_, err = loadSharedProgram(source, f, false, hash)
	require.Error(t, err, "a hash collision must not bypass ELF validation")
}
