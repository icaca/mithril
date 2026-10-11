package sealevel

import (
	"encoding/binary"
	"testing"

	"github.com/sonicfromnewyoke/mithril/pkg/accounts"
	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	feat "github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
	"github.com/stretchr/testify/require"
)

func TestCpiSerializedLengthClearedOnFinish(t *testing.T) {
	old := sbpf.UsePool
	sbpf.UsePool = true
	t.Cleanup(func() { sbpf.UsePool = old })
	ctx := &ExecutionCtx{Features: *feat.NewFeaturesDefault(), ComputeMeter: cu.NewComputeMeter(100000)}
	vm := sbpf.NewInterpreter(&sbpf.Program{TextVA: sbpf.VaddrProgram}, &sbpf.VMOpts{
		HeapMax: 32768, Input: make([]byte, 256), Context: ctx, ComputeMeter: &ctx.ComputeMeter,
	})
	// A read-only view must not mark the length header dirty for the syscall.
	heap, err := vm.Translate(sbpf.VaddrHeap, 16, false)
	require.NoError(t, err)
	t.Cleanup(func() {
		vm.Finish()
		require.Equal(t, make([]byte, 16), heap)
	})
	caller, err := callerAccountFromAccountInfoC(vm, ctx, 0, 0, SolAccountInfoC{
		LamportsAddr: sbpf.VaddrInput + 128, OwnerAddr: sbpf.VaddrInput + 160,
		DataAddr: sbpf.VaddrHeap + 8, DataLen: 0,
	}, sbpf.VaddrInput, false)
	require.NoError(t, err)
	callee := &BorrowedAccount{Account: &accounts.Account{Data: make([]byte, 8)}}
	require.NoError(t, updateCallerAccount(vm, &caller, callee, false))
	require.Equal(t, uint64(8), binary.LittleEndian.Uint64(heap))
}

func TestSyscallParameterAddressRangeRestricted(t *testing.T) {
	features := feat.NewFeaturesDefault()
	execCtx := &ExecutionCtx{Features: *features}

	require.False(t, syscallParameterAddressRangeRestricted(execCtx, sbpf.VaddrInput-1, 1))

	features.EnableFeature(feat.SyscallParameterAddressRestrictions, 0)
	require.True(t, syscallParameterAddressRangeRestricted(execCtx, sbpf.VaddrInput, 0))
	require.True(t, syscallParameterAddressRangeRestricted(execCtx, sbpf.VaddrInput-1, 1))
	require.False(t, syscallParameterAddressRangeRestricted(execCtx, sbpf.VaddrInput-2, 1))
}

func TestCheckCpiDataLengthSyscallParameterAddressRestrictions(t *testing.T) {
	features := feat.NewFeaturesDefault()
	execCtx := &ExecutionCtx{Features: *features}

	require.NoError(t, checkCpiDataLength(execCtx, MaxPermittedDataIncrease+2, 1, true))

	features.EnableFeature(feat.SyscallParameterAddressRestrictions, 0)
	require.NoError(t, checkCpiDataLength(execCtx, 1, 1, true))
	require.ErrorIs(t, checkCpiDataLength(execCtx, 2, 1, true), InstrErrInvalidRealloc)
	require.NoError(t, checkCpiDataLength(execCtx, MaxPermittedDataIncrease+1, 1, false))
	require.ErrorIs(t, checkCpiDataLength(execCtx, MaxPermittedDataIncrease+2, 1, false), InstrErrInvalidRealloc)
}

func TestTranslateSerializedAccountDataDirectMappingSkipsSerializedBytes(t *testing.T) {
	features := feat.NewFeaturesDefault()
	features.EnableFeature(feat.VirtualAddressSpaceAdjustments, 0)
	features.EnableFeature(feat.AccountDataDirectMapping, 0)
	execCtx := &ExecutionCtx{Features: *features}

	meter := cu.NewComputeMeter(1)
	vm := sbpf.NewInterpreter(&sbpf.Program{TextVA: sbpf.VaddrProgram, Funcs: map[uint32]int64{}}, &sbpf.VMOpts{
		Input:        []byte{0x42},
		Context:      execCtx,
		ComputeMeter: &meter,
	})
	defer vm.Finish()

	data, err := translateSerializedAccountData(vm, execCtx, sbpf.VaddrInput+42, 1)
	require.NoError(t, err)
	require.Nil(t, data)

	features.DisableFeature(feat.AccountDataDirectMapping)
	_, err = translateSerializedAccountData(vm, execCtx, sbpf.VaddrInput+42, 1)
	require.Error(t, err)
}
