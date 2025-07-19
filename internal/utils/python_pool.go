package utils

import (
	"errors"
	"sync"
)

// PythonPool manages a pool of PythonHelper instances.
type PythonPool struct {
	pool    chan *PythonHelper
	maxSize int
	factory func() (*PythonHelper, error)
	mu      sync.Mutex
}

// NewPythonPool creates a new pool of Python helpers.
func NewPythonPool(maxSize int, factory func() (*PythonHelper, error)) (*PythonPool, error) {
	if maxSize <= 0 {
		return nil, errors.New("pool size must be positive")
	}

	p := &PythonPool{
		pool:    make(chan *PythonHelper, maxSize),
		maxSize: maxSize,
		factory: factory,
	}

	for i := 0; i < maxSize; i++ {
		helper, err := factory()
		if err != nil {
			// If we can't create all helpers, close the ones we did create.
			p.Close()
			return nil, err
		}
		p.pool <- helper
	}

	return p, nil
}

// Get borrows a PythonHelper from the pool.
func (p *PythonPool) Get() (*PythonHelper, error) {
	helper, ok := <-p.pool
	if !ok {
		return nil, errors.New("pool is closed")
	}
	return helper, nil
}

// Put returns a PythonHelper to the pool.
func (p *PythonPool) Put(helper *PythonHelper) {
	if helper == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pool == nil {
		// Pool is closed, just close the helper
		helper.Close()
		return
	}

	select {
	case p.pool <- helper:
		// Helper returned to pool
	default:
		// Pool is full, close this helper
		helper.Close()
	}
}

// Close closes all Python helpers in the pool.
func (p *PythonPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pool == nil {
		return
	}

	close(p.pool)
	for helper := range p.pool {
		helper.Close()
	}
	p.pool = nil
}