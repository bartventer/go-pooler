# go-pooler

[![Go Reference](https://pkg.go.dev/badge/github.com/bartventer/go-pooler.svg)](https://pkg.go.dev/github.com/bartventer/go-pooler)
[![Release](https://img.shields.io/github/release/bartventer/go-pooler.svg)](https://github.com/bartventer/go-pooler/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/bartventer/go-pooler)](https://goreportcard.com/report/github.com/bartventer/go-pooler)
[![codecov](https://codecov.io/gh/bartventer/go-pooler/graph/badge.svg?token=6i0Pr1GFek)](https://codecov.io/gh/bartventer/go-pooler)
[![Tests](https://github.com/bartventer/go-pooler/actions/workflows/default.yml/badge.svg)](https://github.com/bartventer/go-pooler/actions/workflows/default.yml)
![GitHub issues](https://img.shields.io/github/issues/bartventer/go-pooler)
[![License](https://img.shields.io/github/license/bartventer/go-pooler.svg)](LICENSE)

The `go-pooler` package provides a simple and generic resource pooler for managing reusable resources in Go. It is designed to handle resource acquisition, release, and health checks efficiently, making it suitable for high-concurrency applications.

## Features

 - **Acquiring and Releasing Resources by Key**: Manage resources using unique keys.
 - **Configurable Maximum Number of Open Resources**: Limit the number of open resources.
 - **Periodic Health Checks**: Automatically perform health checks and resource cleanup at configurable intervals.
 - **Pool Statistics**: Gather statistics about the pool's usage, such as the number of open resources and wait times.

# Installation

To install the package, use:

```bash
go get github.com/bartventer/go-pooler
```

# Basic Usage

To create a new pool, define a resource that implements the Reusable interface and use the NewPool function:

```go
package main

import (
	"context"

	"github.com/bartventer/go-pooler"
)

type Worker struct{}
type WorkerPool = pooler.Pool[*Worker]

func (w *Worker) Close() error                          { return nil }
func (w *Worker) PingContext(ctx context.Context) error { return nil }

func WorkerFactory() (*Worker, error) {
	return &Worker{}, nil
}

func NewWorkerPool(ctx context.Context, opts ...pooler.Option) *WorkerPool {
	return pooler.NewPool(ctx, WorkerFactory, opts...)
}

func main() {
	ctx := context.Background()
	p := NewWorkerPool(ctx, pooler.WithMaxOpenResources(1))

	_, err := p.Acquire(ctx, "key1")
	if err != nil {
		panic(err)
	}
	defer p.Release("key1")

	// Use the worker...
}
```

# Documentation

Refer to the [GoDoc](https://pkg.go.dev/github.com/bartventer/go-pooler) for detailed documentation and examples.

## License

This project is licensed under the Apache License, Version 2.0 - see the [LICENSE](LICENSE) file for details.