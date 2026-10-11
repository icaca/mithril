package sealevel

import (
	"bytes"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/sonicfromnewyoke/mithril/fixtures"
	"github.com/sonicfromnewyoke/mithril/pkg/accounts"
	"github.com/sonicfromnewyoke/mithril/pkg/accountsdb"
	a "github.com/sonicfromnewyoke/mithril/pkg/addresses"
	"github.com/sonicfromnewyoke/mithril/pkg/cu"
	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecutionRejectsOtherBankProgramCache(t *testing.T) {
	for _, kind := range []string{"unbound", "same-slot-fork", "different-features"} {
		t.Run(kind, func(t *testing.T) {
			elf := fixtures.Load(t, "sbpf", "spl_memo-1.0.0.so")
			key := solana.PublicKey{79}
			program := accounts.Account{Key: key, Owner: a.BpfLoader2Addr, Executable: true, Lamports: 10000000, Data: elf}
			ctx := programCacheExecCtx(t, program)
			run := func() error {
				ctx.TransactionContext = NewTransactionCtx(*NewTransactionAccounts([]accounts.Account{program}), 5, 64)
				ctx.TransactionContext.ComputeBudgetLimits = &ComputeBudgetLimits{ComputeUnitLimit: 100000, UpdatedHeapBytes: 32768}
				ctx.ComputeMeter = cu.NewComputeMeter(100000)
				return ctx.ProcessInstruction([]byte("cache binding"), nil, []uint64{0})
			}
			require.NoError(t, run())
			used := ctx.ComputeMeter.Used()
			poison := &accountsdb.ProgramCacheEntry{DeploymentSlot: 1336} // nil executable: using it would panic
			f := features.NewFeaturesDefault()
			switch kind {
			case "same-slot-fork":
				poison.BindSource([]byte("another fork's program"), f)
			case "different-features":
				f.EnableFeature(features.VirtualAddressSpaceAdjustments, 0)
				poison.BindSource(elf, f)
			}
			ctx.SlotCtx.AccountsDb.AddProgramToCache(key, poison)
			require.NoError(t, run())
			require.Equal(t, used, ctx.ComputeMeter.Used())
		})
	}
}

func TestWarmCacheCannotBypassDeploymentSlot(t *testing.T) {
	key, dataKey := solana.PublicKey{80}, solana.PublicKey{81}
	var encoded bytes.Buffer
	state := UpgradeableLoaderState{Type: UpgradeableLoaderStateTypeProgram, Program: UpgradeableLoaderStateProgram{ProgramDataAddress: dataKey}}
	require.NoError(t, state.MarshalWithEncoder(bin.NewBinEncoder(&encoded)))
	program := accounts.Account{Key: key, Owner: a.BpfLoaderUpgradeableAddr, Executable: true, Lamports: 10000000, Data: encoded.Bytes()}
	ctx := programCacheExecCtx(t, program)
	ctx.SlotCtx.Slot = 100
	var data bytes.Buffer
	state = UpgradeableLoaderState{Type: UpgradeableLoaderStateTypeProgramData, ProgramData: UpgradeableLoaderStateProgramData{Slot: 100}}
	require.NoError(t, state.MarshalWithEncoder(bin.NewBinEncoder(&data)))
	require.NoError(t, ctx.Accounts.SetAccount((*[32]byte)(&dataKey), &accounts.Account{Key: dataKey, Owner: a.BpfLoaderUpgradeableAddr, Lamports: 10000000, Data: data.Bytes()}))
	// The cache describes a previous bank. Its older deployment slot must not
	// hide this bank's deployment, which cannot be invoked in the same slot.
	ctx.SlotCtx.AccountsDb.AddProgramToCache(dataKey, &accountsdb.ProgramCacheEntry{DeploymentSlot: 99})
	err := ctx.ProcessInstruction(nil, nil, []uint64{0})
	require.ErrorIs(t, err, InstrErrInvalidAccountData)
}

func TestWarmCacheCannotBypassLoaderV4Retraction(t *testing.T) {
	key := solana.PublicKey{82}
	state := LoaderV4State{Slot: 99, Status: LoaderV4StatusRetracted}
	program := accounts.Account{Key: key, Owner: a.LoaderV4Addr, Executable: true, Lamports: 10000000, Data: state.Marshal()}
	ctx := programCacheExecCtx(t, program)
	ctx.SlotCtx.Slot = 100
	require.NoError(t, ctx.Accounts.SetAccount((*[32]byte)(&key), &program))
	ctx.SlotCtx.AccountsDb.AddProgramToCache(key, &accountsdb.ProgramCacheEntry{DeploymentSlot: 99})
	err := ctx.ProcessInstruction(nil, nil, []uint64{0})
	require.ErrorIs(t, err, InstrErrUnsupportedProgramId)
}

// programCacheExecCtx keeps the upstream cache regression cases on our existing
// execution fixtures without importing validator benchmark infrastructure.
func programCacheExecCtx(t *testing.T, program accounts.Account) *ExecutionCtx {
	_, ctx := newSystemProgramTestExecCtx(t, NewTransactionAccounts([]accounts.Account{program}))
	ctx.Log = &LogRecorder{}
	ctx.TransactionContext.ComputeBudgetLimits = &ComputeBudgetLimits{ComputeUnitLimit: 100000, UpdatedHeapBytes: 32768}
	db := &accountsdb.AccountsDb{}
	db.InitCaches()
	t.Cleanup(db.ProgramCache.Close)
	t.Cleanup(db.CommonAcctsCache.Close)
	ctx.SlotCtx = &SlotCtx{Accounts: ctx.Accounts, AccountsDb: db, Slot: 1337}
	return ctx
}
