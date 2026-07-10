package health

// WeightScheme defines the scoring weights for each component.
type WeightScheme struct {
	CPU    int
	Memory int
	Disk   int
	GPU    int // Used for both GPU and NPU
}

// Predefined weight schemes.
var (
	// CPUOnlyScheme: no GPU/NPU (CPU 30 + Memory 40 + Disk 30 = 100)
	CPUOnlyScheme = WeightScheme{CPU: 30, Memory: 40, Disk: 30, GPU: 0}

	// Accelerated8CardScheme: 8-card server (CPU 10 + Memory 20 + Disk 10 + GPU 60 = 100)
	Accelerated8CardScheme = WeightScheme{CPU: 10, Memory: 20, Disk: 10, GPU: 60}

	// Accelerated4CardScheme: 4-card server (same as 8-card for now)
	Accelerated4CardScheme = WeightScheme{CPU: 10, Memory: 20, Disk: 10, GPU: 60}
)

// GetScheme returns the weight scheme for the given scheme name.
func GetScheme(name string) WeightScheme {
	switch name {
	case "cpu_only":
		return CPUOnlyScheme
	case "accelerated_8card":
		return Accelerated8CardScheme
	case "accelerated_4card":
		return Accelerated4CardScheme
	default:
		return CPUOnlyScheme
	}
}
