// Package worker provides a global worker pool for managing concurrent tasks.
package worker

import (
	"context"
	"log"
	"web-search-api-for-llms/internal/extractor"
)

// Job represents a task to be executed by a worker.
type Job struct {
	URL        string
	Endpoint   string
	MaxChars   *int
	ResultChan chan *extractor.ExtractedResult
	Context    context.Context
}

// WorkerPool manages a pool of workers and a queue of jobs.
type WorkerPool struct {
	JobQueue   chan Job
	Dispatcher *extractor.Dispatcher
	PoolSize   int
}

// NewWorkerPool creates and starts a new worker pool.
func NewWorkerPool(dispatcher *extractor.Dispatcher, poolSize int, queueSize int) *WorkerPool {
	jobQueue := make(chan Job, queueSize)
	return &WorkerPool{
		JobQueue:   jobQueue,
		Dispatcher: dispatcher,
		PoolSize:   poolSize,
	}
}

// Start initializes the worker pool and starts the worker goroutines.
func (wp *WorkerPool) Start() {
	for i := 0; i < wp.PoolSize; i++ {
		go func(workerID int) {
			log.Printf("Worker %d started", workerID)
			for job := range wp.JobQueue {
				log.Printf("Worker %d processing job for URL: %s", workerID, job.URL)
				result, err := wp.Dispatcher.DispatchAndExtractWithContext(job.URL, job.Endpoint, job.MaxChars)
				if err != nil {
					result = &extractor.ExtractedResult{
						URL:                   job.URL,
						ProcessedSuccessfully: false,
						Error:                 err.Error(),
					}
				}
				job.ResultChan <- result
			}
			log.Printf("Worker %d stopped", workerID)
		}(i)
	}
}

// Stop gracefully shuts down the worker pool.
func (wp *WorkerPool) Stop() {
	log.Println("Stopping worker pool...")
	close(wp.JobQueue)
}