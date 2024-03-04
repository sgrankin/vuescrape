package vueclient

import "sync"

// An Atom holds a value which can be updated atomically.
// Watchers may be registered to receive updates.
type Atom[T any] struct {
	mu       sync.RWMutex
	v        T
	watchers []func(T, T)
}

func NewAtom[T any](v T) *Atom[T] { return &Atom[T]{v: v} }

// Load fetches the current value.
func (a *Atom[T]) Load() T {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.v
}

// Reset sets the current value.
func (a *Atom[T]) Reset(v T) T {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, a.v = a.v, v
	// TODO: should watchers be called outside of the lock?
	for _, w := range a.watchers {
		w(v, a.v)
	}
	return v
}

// Watch registers a function f that will be called whenevr the value is set.
func (a *Atom[T]) Watch(f func(old T, new T)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.watchers = append(a.watchers, f)
}
