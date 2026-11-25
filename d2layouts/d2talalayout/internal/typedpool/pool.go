// Package typedpool provides a type-safe wrapper around sync.Pool.
package typedpool

import "sync"

// MaxRetainedEntries bounds what an object put into a Pool may keep: the
// elements of each slice or map it holds (pools of per-node arrays name their
// own bound). TinyGo's sync.Pool, which d2's wasm build uses, is a plain list
// that never frees: everything put into it stays allocated, and is scanned
// by every collection, for the life of the program. An object that grew
// beyond the bound is not put back; the collector frees it, as it frees the
// allocation the pool replaces.
const MaxRetainedEntries = 4096

// Pool keeps sync.Pool's allocation behavior while making both Get and Put
// type-safe at their call sites.
type Pool[T any] struct {
	pool sync.Pool
}

// New constructs a typed pool whose allocator returns a fresh T.
func New[T any](newValue func() T) *Pool[T] {
	return &Pool[T]{
		pool: sync.Pool{
			New: func() any { return newValue() },
		},
	}
}

// Get selects an arbitrary item from the pool or calls its allocator.
func (pool *Pool[T]) Get() T {
	return pool.pool.Get().(T)
}

// Put adds value to the pool.
func (pool *Pool[T]) Put(value T) {
	pool.pool.Put(value)
}
