/*
Package pooler provides a generic resource pooler for managing reusable resources. It is designed
to handle resource acquisition, release, and health checks efficiently, making it suitable for
high-concurrency applications.

# Features:

The following features are provided by the pooler package:

  - Acquiring and releasing resources by key.
  - Limiting the number of open resources.
  - Automatic health checks and resource cleanup at configurable intervals.
  - Statistics for monitoring the pool.

# Usage

For example usage, see the documentation for the [Pool] type.
*/
package pooler

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"sync"
	"time"
)

// Reusable defines the interface for a resource that can be pooled.
// The resource must be able to close itself and perform a health check.
type Reusable interface {
	// Close closes the resource.
	Close() error
	// PingContext performs a health check on the resource.
	PingContext(ctx context.Context) error
}

// Stats defines the statistics for a pool.
type Stats struct {
	MaxOpenResources int           // maximum number of resources
	OpenResources    int           // number of open resources
	WaitCount        int64         // number of waiters
	WaitDuration     time.Duration // total time blocked waiting for a resource (from the time Acquire is called)
}

// FactoryFunc is a function type that creates a new reusable resource.
type FactoryFunc[T Reusable] func() (T, error)

// Pooler defines the interface for acquiring and releasing resources.
// The pooler can acquire a resource by key, release a resource by key, check if a resource exists by key,
// release all resources in the pool, and return the statistics for the pool.
type Pooler[T Reusable] interface {
	// Acquire acquires a resource by key.
	Acquire(ctx context.Context, key string) (T, error)
	// Release releases a resource by key.
	Release(key string)
	// Contains reports whether a resource exists by key.
	Contains(key string) bool
	// ReleaseAll releases all resources in the pool.
	ReleaseAll()
	// Stats returns the statistics for the pool.
	Stats() Stats
}

// Options defines the configuration options for a pool.
type Options struct {
	MaxOpenResources    int           // maximum number of open resources
	HealthCheckInterval time.Duration // interval for health checks
}

// apply applies the configuration options to the pool options.
func (o *Options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}

	// Ensure options are within valid ranges.
	o.MaxOpenResources = cmp.Or(max(o.MaxOpenResources, 0), defaultMaxOpenResources)
	o.HealthCheckInterval = cmp.Or(max(o.HealthCheckInterval, 0), defaultHealthCheckInterval)
}

// Option is a function type that configures an [Options] instance.
type Option func(*Options)

// WithMaxOpenResources sets the maximum number of allowed open resources.
func WithMaxOpenResources(n int) Option {
	return func(o *Options) {
		o.MaxOpenResources = n
	}
}

// WithHealthCheckInterval sets the interval for health checks.
func WithHealthCheckInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.HealthCheckInterval = interval
	}
}

// Pool implements [Pooler] for managing a pool of resources.
// It is safe for concurrent use by multiple goroutines.
type Pool[T Reusable] struct {
	mu           sync.RWMutex   // mu protects resources.
	resources    map[string]T   // map of resources by key.
	factory      FactoryFunc[T] // factory function to create resources.
	options      Options        // configuration options.
	releaseCh    chan struct{}  // channel to release resources.
	waitCount    int64          // number of waiters.
	waitDuration time.Duration  // total time blocked waiting for a resource.
}

// Default options.
const (
	defaultMaxOpenResources    = 10
	defaultHealthCheckInterval = 1 * time.Minute
)

// ErrFactoryError is returned when the factory function fails to create a resource.
var ErrFactoryError = errors.New("pool: factory error")

// NewPool creates a new pool of resources.
// It accepts a context, a factory function to create resources, and optional configuration functions.
func NewPool[T Reusable](ctx context.Context, factory FactoryFunc[T], opts ...Option) *Pool[T] {
	options := Options{}
	options.apply(opts...)

	p := &Pool[T]{
		resources: make(map[string]T),
		factory:   factory,
		options:   options,
		releaseCh: make(chan struct{}, 1), // Buffered channel to avoid blocking.
	}
	go p.startHealthCheck(ctx)
	return p
}

// startHealthCheck starts the health check process for the pool.
// It periodically pings each resource and removes unhealthy ones.
func (p *Pool[T]) startHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(p.options.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.RLock()
			for key, resource := range p.resources {
				go func(key string, resource T) {
					if err := resource.PingContext(ctx); err != nil {
						p.mu.Lock()
						defer p.mu.Unlock()
						resource.Close()
						delete(p.resources, key)
					}
				}(key, resource)
			}
			p.mu.RUnlock()
		}
	}
}

// Acquire acquires a resource by key.
// If the resource does not exist, it creates a new one using the factory function.
func (p *Pool[T]) Acquire(ctx context.Context, key string) (T, error) {
	for {
		start := time.Now()
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		default:
		}

		p.mu.RLock()
		if conn, exists := p.resources[key]; exists {
			p.mu.RUnlock()
			return conn, nil
		}

		if len(p.resources) >= p.options.MaxOpenResources {
			p.mu.RUnlock()
			p.mu.Lock()
			p.waitCount++
			p.mu.Unlock()
			select {
			case <-p.releaseCh:
			case <-ctx.Done():
				p.mu.Lock()
				p.waitCount--
				p.mu.Unlock()
				var zero T
				return zero, ctx.Err()
			}
			p.mu.Lock()
			p.waitCount--
			p.waitDuration += time.Since(start)
			p.mu.Unlock()
			continue
		}
		p.mu.RUnlock()

		conn, err := p.factory()
		if err != nil {
			var zero T
			return zero, errors.Join(ErrFactoryError, fmt.Errorf("key: %s, error: %w", key, err))
		}

		p.mu.Lock()
		p.resources[key] = conn
		p.waitDuration += time.Since(start)
		p.mu.Unlock()
		return conn, nil
	}
}

// Release releases a resource by key.
// It closes the resource and removes it from the pool.
func (p *Pool[T]) Release(key string) {
	p.mu.Lock()
	if conn, exists := p.resources[key]; exists {
		conn.Close()
		delete(p.resources, key)
	}
	p.mu.Unlock()
	select {
	case p.releaseCh <- struct{}{}:
	default:
	}
}

// Contains reports whether a resource exists by key.
func (p *Pool[T]) Contains(key string) bool {
	p.mu.RLock()
	_, exists := p.resources[key]
	p.mu.RUnlock()
	return exists
}

// ReleaseAll releases all resources in the pool.
func (p *Pool[T]) ReleaseAll() {
	p.mu.Lock()
	for key, conn := range p.resources {
		conn.Close()
		delete(p.resources, key)
	}
	p.mu.Unlock()
	select {
	case p.releaseCh <- struct{}{}:
	default:
	}
}

// Stats returns a snapshot of the pool's statistics.
func (p *Pool[T]) Stats() Stats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return Stats{
		MaxOpenResources: p.options.MaxOpenResources,
		OpenResources:    len(p.resources),
		WaitCount:        p.waitCount,
		WaitDuration:     p.waitDuration,
	}
}

// All returns an iterator that yields All resources in the pool.
func (p *Pool[T]) All() iter.Seq2[string, T] {
	p.mu.RLock()
	all := maps.All(p.resources)
	p.mu.RUnlock()
	return all
}

// Walk returns a "pull iterator" that yields all resources in the pool.
func (p *Pool[T]) Walk() (next func() (key string, value T, ok bool), stop func()) {
	return iter.Pull2(p.All())
}
