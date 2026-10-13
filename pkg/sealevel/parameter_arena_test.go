package sealevel

import (
	"bytes"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/sonicfromnewyoke/mithril/pkg/accounts"
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/stretchr/testify/require"
)

func parameterTestContext(data []byte) *ExecutionCtx {
	program := solana.PublicKey{1}
	accts := NewTransactionAccounts([]accounts.Account{
		{Key: program, Executable: true},
		{Key: solana.PublicKey{2}, Owner: program, Lamports: 123, Data: data, RentEpoch: 456},
		{Key: solana.PublicKey{3}, Owner: program, Lamports: 789, Executable: true},
	})
	tx := NewTransactionCtx(*accts, 5, 64)
	tx.InstructionTrace[0].Configure([]uint64{0}, []InstructionAccount{
		{IndexInTransaction: 1, IndexInCaller: 0, IndexInCallee: 0, IsSigner: true, IsWritable: true},
		{IndexInTransaction: 1, IndexInCaller: 0, IndexInCallee: 0, IsSigner: true, IsWritable: true},
		{IndexInTransaction: 2, IndexInCaller: 2, IndexInCallee: 2},
	}, []byte("instruction"))
	tx.InstructionStack = []uint64{0}
	return &ExecutionCtx{TransactionContext: tx, SlotCtx: &SlotCtx{}, Features: *features.NewFeaturesDefault()}
}

func TestParameterArenaSerializationMatchesFresh(t *testing.T) {
	for _, mode := range []string{"aligned", "vasa", "direct", "unaligned", "unaligned-direct"} {
		t.Run(mode, func(t *testing.T) {
			var arena ParameterArena
			serialize := serializeParametersAligned
			if mode == "unaligned" || mode == "unaligned-direct" {
				serialize = serializeParametersUnaligned
			}
			for _, size := range []int{33, 1, 0, 65} {
				ctx := parameterTestContext(bytes.Repeat([]byte{0x42}, size))
				if mode == "vasa" || mode == "direct" || mode == "unaligned-direct" {
					ctx.Features.EnableFeature(features.VirtualAddressSpaceAdjustments, 0)
				}
				if mode == "direct" || mode == "unaligned-direct" {
					ctx.Features.EnableFeature(features.AccountDataDirectMapping, 0)
				}
				want, wantLens, wantOffset, wantMetadata, _, err := serialize(ctx)
				require.NoError(t, err)

				ctx.SlotCtx.TransactionParameterArena = &arena
				ctx.IsSimulation = true
				got, lens, offset, metadata, _, err := serialize(ctx)
				require.NoError(t, err)
				require.Equal(t, want, got, "mode=%s account data length=%d", mode, size)
				require.Equal(t, wantLens, lens)
				require.Equal(t, wantOffset, offset)
				require.Equal(t, wantMetadata, metadata)

				// A program may dirty every byte before the next, smaller transaction.
				for i := range got {
					got[i] = 0xa5
				}
				arena.Reset()
			}
		})
	}
}

func TestParameterArenaKeepsInvocationBuffers(t *testing.T) {
	var arena ParameterArena
	parent := parameterTestContext(bytes.Repeat([]byte{0x11}, 33))
	parent.SlotCtx.TransactionParameterArena = &arena
	parentBytes, _, _, _, _, err := serializeParametersAligned(parent)
	require.NoError(t, err)
	saved := bytes.Clone(parentBytes)

	child := parameterTestContext(bytes.Repeat([]byte{0x22}, 65))
	child.SlotCtx.TransactionParameterArena = &arena
	childBytes, _, _, _, _, err := serializeParametersAligned(child)
	require.NoError(t, err)
	clear(childBytes)
	require.Equal(t, saved, parentBytes, "a CPI buffer must not overwrite its caller")

	arena.Reset()
	reused, _, _, _, _, err := serializeParametersAligned(parent)
	require.NoError(t, err)
	require.Equal(t, saved, reused)
	require.True(t, &parentBytes[0] == &reused[0], "the next transaction reuses the allocation")
}
