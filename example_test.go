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
package pooler_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bartventer/go-pooler"
)

func ExamplePool_Acquire_release() {
	ctx := context.Background()
	p := NewWorkerPool(ctx)

	resource, err := p.Acquire(ctx, "key1")
	if err != nil {
		fmt.Println("Failed to acquire resource:", err)
		return
	}
	fmt.Printf("Worker acquired: %+v\n", resource)

	p.Release("key1")
	fmt.Println("Worker released")

	// Output:
	// Worker acquired: &{closed:false mu:{state:0 sema:0}}
	// Worker released
}

func ExamplePool_Acquire_concurrent() {
	ctx := context.Background()
	p := NewWorkerPool(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i)
			_, _ = p.Acquire(ctx, key)
			fmt.Println("Worker acquired:", key)
		}(i)
	}

	wg.Wait()

	// Unordered output:
	// Worker acquired: key0
	// Worker acquired: key1
	// Worker acquired: key2
}

func ExamplePool_healthCheck() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewWorkerPool(ctx, pooler.WithHealthCheckInterval(1*time.Second))
	worker, _ := p.Acquire(ctx, "key1")
	worker.Close()

	// Initial stats
	stats := p.Stats()
	fmt.Println("OpenResources before health check:", stats.OpenResources)

	// Simulate a health check
	time.Sleep(2 * time.Second)

	stats = p.Stats()
	fmt.Println("OpenResources after health check:", stats.OpenResources)

	// Output:
	// OpenResources before health check: 1
	// OpenResources after health check: 0
}

func ExamplePool_ReleaseAll() {
	ctx := context.Background()
	p := NewWorkerPool(ctx)

	for i := 0; i < 3; i++ {
		_, _ = p.Acquire(ctx, fmt.Sprintf("key%d", i))
	}
	stats := p.Stats()
	fmt.Println("OpenResources before ReleaseAll:", stats.OpenResources)

	p.ReleaseAll()
	stats = p.Stats()
	fmt.Println("OpenResources after ReleaseAll:", stats.OpenResources)

	// Output:
	// OpenResources before ReleaseAll: 3
	// OpenResources after ReleaseAll: 0
}

func ExamplePool_All() {
	ctx := context.Background()
	p := NewWorkerPool(ctx)

	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("key%d", i)
		_, _ = p.Acquire(ctx, key)
	}

	for key, worker := range p.All() {
		fmt.Printf("%s %+v\n", key, worker)
	}

	// Unordered output:
	// key0 &{closed:false mu:{state:0 sema:0}}
	// key1 &{closed:false mu:{state:0 sema:0}}
	// key2 &{closed:false mu:{state:0 sema:0}}
}

// ExamplePool_Walk demonstrates iterating over all resources using the Walk method.
func ExamplePool_Walk() {
	ctx := context.Background()
	p := NewWorkerPool(ctx)

	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("key%d", i)
		_, _ = p.Acquire(ctx, key)
	}

	next, stop := p.Walk()
	defer stop()
	for {
		key, worker, ok := next()
		if !ok {
			break
		}
		fmt.Printf("%s %+v\n", key, worker)
	}

	// Unordered output:
	// key0 &{closed:false mu:{state:0 sema:0}}
	// key1 &{closed:false mu:{state:0 sema:0}}
	// key2 &{closed:false mu:{state:0 sema:0}}
}
