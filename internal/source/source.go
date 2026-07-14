package source

// Source is the common interface that all data sources implement.
// It provides identity and availability information only.
//
// Source-specific typed methods (e.g. proc.Stat, ipmi.SDR) are declared on
// each source package's own interface/struct; this interface exists for
// future visibility/registry use (deferred per design decision F) and is
// intentionally minimal so that adding a source does not require editing a
// shared interface declaration.
type Source interface {
	Name() string
	Available() bool
}
