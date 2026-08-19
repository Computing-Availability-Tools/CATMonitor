package health

// WeightScheme defines the scoring weights for each component.
type WeightScheme struct {
	CPU     int
	Memory  int
	Disk    int
	GPU     int // Used for both GPU and NPU
	Network int
	Chassis int
}

// Predefined weight schemes.
var (
	// CPUOnlyScheme: no GPU/NPU (CPU 25 + Memory 25 + Disk 30 + Network 10 + Chassis 10 = 100)
	CPUOnlyScheme = WeightScheme{CPU: 25, Memory: 25, Disk: 30, GPU: 0, Network: 10, Chassis: 10}

	// Accelerated2CardScheme: 1-2 NPU cards (CPU 20 + Memory 20 + Disk 20 + GPU 20 + Network 10 + Chassis 10 = 100)
	Accelerated2CardScheme = WeightScheme{CPU: 20, Memory: 20, Disk: 20, GPU: 20, Network: 10, Chassis: 10}

	// Accelerated4CardScheme: 3-4 NPU cards (CPU 15 + Memory 15 + Disk 20 + GPU 30 + Network 10 + Chassis 10 = 100)
	Accelerated4CardScheme = WeightScheme{CPU: 15, Memory: 15, Disk: 20, GPU: 30, Network: 10, Chassis: 10}

	// Accelerated8CardScheme: 5-8 NPU cards (CPU 15 + Memory 15 + Disk 15 + GPU 35 + Network 10 + Chassis 10 = 100)
	Accelerated8CardScheme = WeightScheme{CPU: 15, Memory: 15, Disk: 15, GPU: 35, Network: 10, Chassis: 10}
)

// GetScheme returns the weight scheme for the given scheme name.
func GetScheme(name string) WeightScheme {
	switch name {
	case "cpu_only":
		return CPUOnlyScheme
	case "accelerated_2card":
		return Accelerated2CardScheme
	case "accelerated_4card":
		return Accelerated4CardScheme
	case "accelerated_8card":
		return Accelerated8CardScheme
	default:
		return CPUOnlyScheme
	}
}
