package pooler_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bartventer/go-pooler"
)

type (
	// Worker is a resource that can be pooled.
	Worker struct {
		closed bool
		mu     sync.Mutex
	}
	// WorkerPool is a pool of Worker instances.
	WorkerPool = pooler.Pool[*Worker]
)

// WorkerFactory creates a new Worker instance.
func WorkerFactory() (*Worker, error) {
	return &Worker{}, nil
}

// NewWorkerPool creates a new WorkerPool instance.
func NewWorkerPool(ctx context.Context, opts ...pooler.Option) *WorkerPool {
	return pooler.NewPool(ctx, WorkerFactory, opts...)
}

var _ pooler.Reusable = new(Worker)

func (m *Worker) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *Worker) PingContext(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("worker is closed")
	}
	return nil
}

func ExamplePool() {
	ctx := context.Background()
	p := NewWorkerPool(ctx, pooler.WithMaxOpenResources(1))

	_, err := p.Acquire(ctx, "key1")
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := p.Acquire(ctx, "key2")
		errCh <- err
	}()

	// Wait for the goroutine to block on Acquire
	time.Sleep(100 * time.Millisecond)

	// The waiting goroutine should be in the wait queue
	stats := p.Stats()
	fmt.Println("OpenResources:", stats.OpenResources)
	fmt.Println("WaitCount:", stats.WaitCount)

	// Release a resource to unblock the waiting goroutine
	p.Release("key1")
	wg.Wait()
	close(errCh)

	stats = p.Stats()
	fmt.Println("OpenResources:", stats.OpenResources)
	fmt.Println("WaitCount:", stats.WaitCount)
	fmt.Println("Duration took longer than 100ms:", stats.WaitDuration > 100*time.Millisecond)

	// Output:
	// OpenResources: 1
	// WaitCount: 1
	// OpenResources: 1
	// WaitCount: 0
	// Duration took longer than 100ms: true
}
