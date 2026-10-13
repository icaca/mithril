package sealevel

// ParameterArena reuses serialized parameter buffers across transactions.
// The zero value is ready to use. It must not be shared by concurrent executions.
type ParameterArena struct {
	buffers [][]byte
	next    int
}

func (a *ParameterArena) alloc(size uint64) []byte {
	if a.next == len(a.buffers) {
		a.buffers = append(a.buffers, nil)
	}
	buffer := a.buffers[a.next]
	if uint64(cap(buffer)) < size {
		buffer = make([]byte, size)
		a.buffers[a.next] = buffer
	}
	a.next++
	// Serializers overwrite every byte, including padding and realloc space.
	return buffer[:size:size]
}

// Reset makes buffers available to the next transaction. All consumers must
// have finished or copied their data; buffers remain distinct until Reset.
func (a *ParameterArena) Reset() {
	a.next = 0
}
