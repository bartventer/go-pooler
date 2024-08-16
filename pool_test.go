// Copyright (c) 2024 Bart Venter <bartventer@outlook.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package pooler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockReusable is a mock implementation of the Reusable interface.
type MockReusable struct {
	mock.Mock
}

func (m *MockReusable) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockReusable) PingContext(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// MockFactory is a mock implementation of the Factory function.
func MockFactory() (Reusable, error) {
	return &MockReusable{}, nil
}

func TestNewPool_DefaultOptions(t *testing.T) {
	ctx := context.Background()
	pool := NewPool(ctx, MockFactory)

	assert.Equal(t, defaultMaxOpenResources, pool.options.MaxOpenResources)
	assert.Equal(t, defaultHealthCheckInterval, pool.options.HealthCheckInterval)
}

func TestNewPool_CustomOptions(t *testing.T) {
	ctx := context.Background()
	pool := NewPool(ctx, MockFactory, WithMaxOpenResources(5), WithHealthCheckInterval(2*time.Minute))

	assert.Equal(t, 5, pool.options.MaxOpenResources)
	assert.Equal(t, 2*time.Minute, pool.options.HealthCheckInterval)
}

func TestPool_Acquire(t *testing.T) {
	ctx := context.Background()
	pool := NewPool(ctx, MockFactory)

	resource, err := pool.Acquire(ctx, "key1")
	require.NoError(t, err)
	assert.NotNil(t, resource)
	assert.True(t, pool.Contains("key1"))

	// Acquire the same resource again
	resource2, err := pool.Acquire(ctx, "key1")
	require.NoError(t, err)
	assert.NotNil(t, resource2)

	ctx2, cancel := context.WithCancel(ctx)
	cancel()

	_, err = pool.Acquire(ctx2, "key1")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPool_Acquire_FactoryError(t *testing.T) {
	ctx := context.Background()
	pool := NewPool(ctx, func() (Reusable, error) {
		return nil, errors.New("error")
	})

	resource, err := pool.Acquire(ctx, "key1")
	require.ErrorIs(t, err, ErrFactoryError)
	assert.Nil(t, resource)
}

func TestPool_Release(t *testing.T) {
	mockResource := new(MockReusable)
	mockResource.On("Close").Return(nil)

	ctx := context.Background()
	pool := NewPool(ctx, func() (Reusable, error) {
		return mockResource, nil
	})

	resource, err := pool.Acquire(ctx, "key1")
	require.NoError(t, err)
	assert.NotNil(t, resource)

	pool.Release("key1")
	assert.False(t, pool.Contains("key1"))
	mockResource.AssertCalled(t, "Close")
	mockResource.AssertExpectations(t)
}

func TestPool_Contains(t *testing.T) {
	ctx := context.Background()
	pool := NewPool(ctx, MockFactory)

	assert.False(t, pool.Contains("key1"))

	_, err := pool.Acquire(ctx, "key1")
	require.NoError(t, err)
	assert.True(t, pool.Contains("key1"))
}

func TestPool_ReleaseAll(t *testing.T) {
	mockResource := new(MockReusable)
	mockResource.On("Close").Return(nil)

	ctx := context.Background()
	pool := NewPool(ctx, func() (Reusable, error) {
		return mockResource, nil
	})

	for i := range 5 {
		key := fmt.Sprintf("key%d", i)
		_, err := pool.Acquire(ctx, key)
		require.NoError(t, err)
		assert.True(t, pool.Contains(key))
	}

	pool.ReleaseAll()

	for i := range 5 {
		key := fmt.Sprintf("key%d", i)
		assert.False(t, pool.Contains(key))
	}
}

func TestPool_Stats(t *testing.T) {
	ctx := context.Background()
	pool := NewPool(ctx, MockFactory, WithMaxOpenResources(2))

	stats := pool.Stats()
	assert.Equal(t, 2, stats.MaxOpenResources)
	assert.Equal(t, 0, stats.OpenResources)

	_, _ = pool.Acquire(ctx, "key1")
	_, _ = pool.Acquire(ctx, "key2")

	stats = pool.Stats()
	assert.Equal(t, 2, stats.MaxOpenResources)
	assert.Equal(t, 2, stats.OpenResources)
}

func TestPool_HealthCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockResource := new(MockReusable)
	mockResource.On("PingContext", ctx).Return(errors.New("error"))
	mockResource.On("Close").Return(nil)

	pool := NewPool(ctx, func() (Reusable, error) {
		return mockResource, nil
	}, WithHealthCheckInterval(100*time.Millisecond))

	resource, err := pool.Acquire(ctx, "key1")
	require.NoError(t, err)
	assert.NotNil(t, resource)
	assert.True(t, pool.Contains("key1"))

	// Wait for the health check to run
	time.Sleep(200 * time.Millisecond)

	assert.False(t, pool.Contains("key1"))
	mockResource.AssertExpectations(t)
}

func TestPool_Acquire_MaxResources(t *testing.T) {
	ctx := context.Background()
	mockResource := new(MockReusable)
	mockResource.On("Close").Return(nil)
	pool := NewPool(ctx, func() (Reusable, error) {
		return mockResource, nil
	}, WithMaxOpenResources(1))

	resource1, err := pool.Acquire(ctx, "key1")
	require.NoError(t, err)
	assert.NotNil(t, resource1)
	assert.True(t, pool.Contains("key1"))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = pool.Acquire(ctx, "key2")
	}()

	// Wait for the goroutine to block on Acquire
	time.Sleep(100 * time.Millisecond)

	// The waiting goroutine should be in the wait queue
	stats := pool.Stats()
	assert.False(t, pool.Contains("key2"))
	assert.Equal(t, int64(1), stats.WaitCount)

	// Release resource1 to unblock the waiting goroutine
	pool.Release("key1")
	wg.Wait()

	assert.False(t, pool.Contains("key1"))
	assert.True(t, pool.Contains("key2"))

	stats = pool.Stats()
	assert.Equal(t, int64(0), stats.WaitCount)
	assert.GreaterOrEqual(t, stats.WaitDuration, int64(100*time.Millisecond))
}

func TestPool_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pool := NewPool(ctx, MockFactory)

	cancel()

	_, err := pool.Acquire(ctx, "key1")
	require.ErrorIs(t, err, context.Canceled)
}

func TestPool_ContextCancellation_Waiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pool := NewPool(ctx, MockFactory, WithMaxOpenResources(1))

	_, err := pool.Acquire(ctx, "key1")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, ctxErr := pool.Acquire(ctx, "key2")
		errCh <- ctxErr
	}()

	// Wait for the goroutine to block on Acquire
	time.Sleep(100 * time.Millisecond)

	cancel()
	wg.Wait()
	close(errCh)

	err = <-errCh
	require.ErrorIs(t, err, context.Canceled)
}

func TestPool_Parallel_Acquire_MaxResources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mockResource := new(MockReusable)
	mockResource.On("Close").Return(nil)
	pool := NewPool(ctx, func() (Reusable, error) {
		return mockResource, nil
	}, WithMaxOpenResources(2))

	for i := range 3 {
		t.Run(fmt.Sprintf("goroutine-%d", i), func(t *testing.T) {
			t.Parallel()
			key := fmt.Sprintf("key%d", i)
			_, err := pool.Acquire(ctx, key)
			require.NoError(t, err)
			assert.True(t, pool.Contains(key))

			// Give some time for the goroutine to block on Acquire
			time.Sleep(100 * time.Millisecond)

			// Release the resource to make space for the next goroutine
			pool.Release(key)
			log.Printf("Released resource for key: %s", key)
			assert.False(t, pool.Contains(key))
		})
	}
}

func TestPool_All(t *testing.T) {
	ctx := context.Background()
	pool := NewPool(ctx, MockFactory)

	for i := range 3 {
		key := fmt.Sprintf("key%d", i)
		_, err := pool.Acquire(ctx, key)
		require.NoError(t, err)
	}

	allResources := maps.Collect(pool.All())
	assert.Len(t, allResources, 3)
}

func TestPool_Walk(t *testing.T) {
	ctx := context.Background()
	pool := NewPool(ctx, MockFactory)

	for i := range 3 {
		key := fmt.Sprintf("key%d", i)
		_, err := pool.Acquire(ctx, key)
		require.NoError(t, err)
	}

	var keys []string
	next, stop := pool.Walk()
	defer stop()
	for {
		key, _, ok := next()
		if !ok {
			break
		}
		keys = append(keys, key)
	}

	assert.Len(t, keys, 3)
}
