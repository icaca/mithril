package sealevel

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/maphash"
	"slices"
	"sync"

	"github.com/sonicfromnewyoke/mithril/pkg/features"
	"github.com/sonicfromnewyoke/mithril/pkg/mlog"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf"
	"github.com/sonicfromnewyoke/mithril/pkg/sbpf/loader"
)

type sharedProgramKey struct {
	source   uint64
	size     int
	features [32]byte
}

// Retain compiled programs for the process lifetime, independently of VM caches.
// The lock also prevents concurrent first loads from compiling the same ELF twice.
var sharedPrograms = struct {
	sync.Mutex
	seed     maphash.Seed
	programs map[sharedProgramKey][]*sharedProgram
}{seed: maphash.MakeSeed(), programs: make(map[sharedProgramKey][]*sharedProgram)}

type sharedProgram struct {
	source                      []byte
	program                     *sbpf.Program
	executionErr, deploymentErr error
}

// LoadSharedProgram parses and verifies each ELF/features pair once. Returned
// programs are immutable, including their slices/maps; each Interpreter owns its
// execution state. Both syscall environments are checked before publication so
// neither a runtime hit nor a deployment hit can bypass the other's validation.
func LoadSharedProgram(source []byte, f *features.Features, deployment bool) (*sbpf.Program, error) {
	return loadSharedProgram(source, f, deployment, maphash.Bytes(sharedPrograms.seed, source))
}

type immutableProgramKey struct {
	// A Go pointer keeps the allocation alive, preventing address reuse after GC.
	source     *byte
	size       int
	features   [32]byte
	deployment bool
}

var immutablePrograms = struct {
	sync.Mutex
	programs map[immutableProgramKey]*sbpf.Program
}{programs: make(map[immutableProgramKey]*sbpf.Program)}

// LoadSharedProgramImmutable is LoadSharedProgram with an address-based fast path.
// The caller must never modify source bytes for the lifetime of the process.
// The cache retains their backing allocation as well as the compiled program.
func LoadSharedProgramImmutable(source []byte, f *features.Features, deployment bool) (*sbpf.Program, error) {
	if len(source) == 0 {
		return LoadSharedProgram(source, f, deployment)
	}
	key := immutableProgramKey{source: &source[0], size: len(source), features: programFeaturesHash(f), deployment: deployment}
	immutablePrograms.Lock()
	defer immutablePrograms.Unlock()
	if p := immutablePrograms.programs[key]; p != nil {
		return p, nil
	}
	p, err := LoadSharedProgram(source, f, deployment)
	if err != nil {
		return nil, err
	}
	immutablePrograms.programs[key] = p
	return p, nil
}

func loadSharedProgram(source []byte, f *features.Features, deployment bool, sourceHash uint64) (*sbpf.Program, error) {
	key := sharedProgramKey{source: sourceHash, size: len(source), features: programFeaturesHash(f)}
	sharedPrograms.Lock()
	defer sharedPrograms.Unlock()
	var entry *sharedProgram
	for _, cached := range sharedPrograms.programs[key] {
		if bytes.Equal(cached.source, source) {
			entry = cached
			break
		}
	}
	if entry == nil {
		// Relocation output is registry-independent. Validate its function hashes
		// and imported symbols below, using the same checks as Loader.relocate.
		l, err := loader.NewLoaderWithSyscalls(source, nil, false, f)
		if err != nil {
			return nil, err
		}
		p, err := l.Load()
		if err != nil {
			return nil, err
		}
		if err := p.Verify(); err != nil {
			if deployment {
				mlog.Log.Errorf("failed to verify program: %s", err)
			}
			return nil, err
		}
		entry = &sharedProgram{source: bytes.Clone(source), program: p, executionErr: checkProgramSyscalls(p, f, false), deploymentErr: checkProgramSyscalls(p, f, true)}
		sharedPrograms.programs[key] = append(sharedPrograms.programs[key], entry)
	}
	err := entry.executionErr
	if deployment {
		err = entry.deploymentErr
	}
	if err != nil {
		return nil, err
	}
	return entry.program, nil
}

func checkProgramSyscalls(p *sbpf.Program, f *features.Features, deployment bool) error {
	if p.SbpfVersion.EnableStaticSyscalls() {
		return nil
	}
	for hash := range p.Funcs {
		if _, ok := Syscalls(f, deployment, hash); ok {
			return fmt.Errorf("symbol hash collision with syscall")
		}
	}
	if deployment {
		for _, name := range p.ImportedSyscalls {
			if _, ok := Syscalls(f, true, sbpf.SymbolHash(name)); !ok {
				return fmt.Errorf("deployment check failure - hash did not exist in syscall registry (%s)", name)
			}
		}
	}
	return nil
}

func programFeaturesHash(f *features.Features) [32]byte {
	gates := make([]features.FeatureGate, 0, len(*f))
	for gate := range *f {
		gates = append(gates, gate)
	}
	slices.SortFunc(gates, func(a, b features.FeatureGate) int {
		if c := bytes.Compare(a.Address[:], b.Address[:]); c != 0 {
			return c
		}
		return bytes.Compare([]byte(a.Name), []byte(b.Name))
	})
	h := sha256.New()
	var num [8]byte
	for _, gate := range gates {
		info := (*f)[gate]
		h.Write(gate.Address[:])
		binary.LittleEndian.PutUint64(num[:], uint64(len(gate.Name)))
		h.Write(num[:])
		h.Write([]byte(gate.Name))
		binary.LittleEndian.PutUint64(num[:], info.ActivationSlot)
		h.Write(num[:])
		if info.Enabled {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
	}
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}
