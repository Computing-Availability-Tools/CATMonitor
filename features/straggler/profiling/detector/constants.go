package detector

// Parallel domain name constants.
const (
	tpParallelDomainName           = "tp"
	dpCpParallelDomainName         = "dp_cp"
	dpParallelDomainName           = "dp"
	cpParallelDomainName           = "cp"
	epParallelDomainName           = "exp" // NOTE: "exp" not "ep"
	tpEpParallelDomainName         = "tp_exp"
	ppParallelDomainName           = "pp"
	cpRingParallelDomainName       = "cp_ring"
	cpUlyssesParallelDomainName    = "cp_ulysses"
	defaultGroupParallelDomainName = "default_group"
)

// Minimum ranks required in a group for homogeneous comparison.
const minRanksInGroup = 2

// CSV column name constants.
const (
	zpDeviceColumn       = "ZP_Device"
	zpKernelColumn       = "ZP_Kernel"
	zpHostDataColumn     = "ZP_Host"
	zpDurationColumn     = "ZP_Duration"
	zpBubble             = "ZP_Bubble"
	zpCount              = "ZP_Count"
	dataLoaderDataColumn = "DataLoader"
	stepTimeData         = "StepDuration"
	stepIndex            = "StepIndex"
)

// group_info JSON field names.
const (
	dataFileFieldGroupName   = "group_name"
	dataFileFieldGlobalRanks = "global_ranks"
)

// Detection group priority order (highest first).
var detectionPriority = []string{
	tpParallelDomainName,           // tp
	epParallelDomainName,           // exp
	"ep",                          // ep
	tpEpParallelDomainName,        // tp_exp
	cpParallelDomainName,          // cp
	"cp2",                         // cp2
	cpUlyssesParallelDomainName,   // cp_ulysses
	cpRingParallelDomainName,      // cp_ring
	dpParallelDomainName,          // dp
	dpCpParallelDomainName,        // dp_cp
	"dp_modulo_exp_cp",           // dp_modulo_exp_cp
}
