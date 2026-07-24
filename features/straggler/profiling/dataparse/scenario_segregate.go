package dataparse

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DataParsing is the public entry point for parsing all profiling data.
// It discovers all ascend_pytorch_profiler_*.db files under folderPath and
// processes them concurrently, writing CSV + JSON intermediates to
// folderPath/op_metric/.
func DataParsing(folderPath string) {
	// Ensure output directory.
	os.MkdirAll(filepath.Join(folderPath, "op_metric"), 0755)

	// Discover .db files.
	var dbFiles []string
	filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "[DATA PROCESS] Walk error: %v\n", err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, "ascend_pytorch_profiler_") && strings.HasSuffix(base, ".db") {
			dbFiles = append(dbFiles, path)
		}
		return nil
	})

	if len(dbFiles) == 0 {
		fmt.Fprintf(os.Stderr, "[DATA PROCESS] FATAL: no ascend_pytorch_profiler_*.db files found in %s\n", folderPath)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[DATA PROCESS] Found %d profiler database files\n", len(dbFiles))

	if err := StartProcess(dbFiles, folderPath); err != nil {
		fmt.Fprintf(os.Stderr, "[DATA PROCESS] FATAL: %v\n", err)
		os.Exit(1)
	}
}

// StartProcess coordinates concurrent processing of all database files.
// A semaphore channel limits concurrency to 4 goroutines.
func StartProcess(dbFiles []string, outputFolderPath string) error {
	const maxConcurrency = 4
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

	for _, dbFile := range dbFiles {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := ProcessDatabase(path, outputFolderPath); err != nil {
				fmt.Fprintf(os.Stderr, "[DATA PROCESS] ERROR processing %s: %v\n", path, err)
				errOnce.Do(func() { firstErr = err })
			}
		}(dbFile)
	}

	wg.Wait()
	return firstErr
}
