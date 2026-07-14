//go:build linux

// Package statfs provides a data source that wraps the statfs(2) system call
// to report filesystem block usage for a given mount path.
//
// statfs is a system call (not a /proc or /sys file read), so this package
// abstracts it with a swappable fetcher seam (SetFetcher) so the Disk
// collector's space_usage metric can be unit-tested with fake filesystem
// data instead of real mount points. The source is a process-wide singleton.
//
// Linux-only: syscall.Statfs_t is platform-specific.
package statfs

import "syscall"

// Statfs holds filesystem usage in bytes for one mount point.
type Statfs struct {
	Total uint64 // total bytes (blocks × bsize)
	Free  uint64 // free bytes (includes reserved)
	Avail uint64 // available bytes (free minus reserved-for-root)
	Used  uint64 // Total - Free
}

// Source is the typed interface for the statfs data source.
type Source interface {
	// Statfs returns usage for the given mount path (e.g. "/").
	Statfs(path string) (*Statfs, error)
	// Available reports whether the source is usable (statfs always works on
	// Linux, so this is always true).
	Available() bool
}

// fetcher is a swappable seam so tests can inject fake statfs results.
type fetcher = func(path string) (*Statfs, error)

// realFetch calls statfs(2) and converts the raw syscall struct to bytes.
func realFetch(path string) (*Statfs, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return nil, err
	}
	b := uint64(s.Bsize)
	total := s.Blocks * b
	free := s.Bfree * b
	avail := s.Bavail * b
	return &Statfs{Total: total, Free: free, Avail: avail, Used: total - free}, nil
}

type defaultSource struct {
	fetch fetcher
}

var defaultSrc = &defaultSource{fetch: realFetch}

func Default() Source { return defaultSrc }

// SetFetcher swaps the fetcher (for tests).
func SetFetcher(f fetcher) { defaultSrc.fetch = f }

// ResetFetcher restores the real statfs fetcher (for test cleanup).
func ResetFetcher() { defaultSrc.fetch = realFetch }

func (s *defaultSource) Available() bool { return true }

func (s *defaultSource) Statfs(path string) (*Statfs, error) {
	return s.fetch(path)
}
