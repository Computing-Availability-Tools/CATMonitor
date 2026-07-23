// Package resource implements NPU resource KPI anomaly detection using
// time+space dual-dimension peer comparison with 15-day historical baselines.
//
// Detection pipeline:
//   CSV parse → 1-min trimmed-mean aggregation → window split →
//   time baseline + space detection → compute-first 2D cross-validation →
//   root-cause bounding → JSON + text report
package resource

import "math"

// =============================================================================
// Raw Data Types
// =============================================================================

// CSVRow is one row of raw CSV data. Each metric is a JSON dict keyed by card ID.
type CSVRow struct {
	Timestamp      int64
	Power          map[int]float64 // cardID → watts
	Temp           map[int]float64 // cardID → celsius
	AICoreFreq     map[int]float64 // cardID → MHz
	AICoreUtil     map[int]float64 // cardID → %
	HBMUtil        map[int]float64 // cardID → %
	TXBandwidth    map[int]float64 // cardID → ?
	RXPfcPkt       map[int]float64 // cardID → packets (cumulative counter)
	RocETxErrPkt   map[int]float64 // cardID → packets (cumulative counter)
	RocEOutOfOrder map[int]float64 // cardID → packets (cumulative counter)
	RocENewPktRty  map[int]float64 // cardID → packets (cumulative counter)
	NICRxAllPkg    map[int]float64 // cardID → packets
	CPUAvg         map[string]string // cpuName → utilization %
}

// TimeSeriesData holds the complete parsed time series split into windows.
type TimeSeriesData struct {
	Rows    []CSVRow // aggregated rows (1 per minute after aggregation)
	CardIDs []int    // all card IDs found in the data
	RawRows []CSVRow // raw rows before aggregation (for counter calculations)
}

// =============================================================================
// Metric Enumeration
// =============================================================================

// MetricName enumerates all NPU resource metrics.
type MetricName string

const (
	MetricTemp           MetricName = "temp"
	MetricPower          MetricName = "power"
	MetricAICoreFreq     MetricName = "aicore_freq"
	MetricAICoreUtil     MetricName = "aicore_util"
	MetricHBMUtil        MetricName = "hbm_util"
	MetricTXBandwidth    MetricName = "tx_bandwidth"
	MetricRXPfcPkt       MetricName = "rx_pfc_pkt"
	MetricRocETxErrPkt   MetricName = "roce_tx_err_pkt"
	MetricRocEOutOfOrder MetricName = "roce_out_of_order"
	MetricRocENewPktRty  MetricName = "roce_new_pkt_rty"
)

// AllMetrics is the ordered list of all metrics for iteration.
var AllMetrics = []MetricName{
	MetricTemp,
	MetricPower,
	MetricAICoreFreq,
	MetricAICoreUtil,
	MetricHBMUtil,
	MetricTXBandwidth,
	MetricRXPfcPkt,
	MetricRocETxErrPkt,
	MetricRocEOutOfOrder,
	MetricRocENewPktRty,
}

// ComputeMetrics lists metrics classified as compute-related.
var ComputeMetrics = map[MetricName]bool{
	MetricTemp:       true,
	MetricPower:      true,
	MetricAICoreFreq: true,
	MetricAICoreUtil: true,
	MetricHBMUtil:    true,
}

// CommunicationMetrics lists metrics classified as communication-related.
var CommunicationMetrics = map[MetricName]bool{
	MetricTXBandwidth:    true,
	MetricRXPfcPkt:       true,
	MetricRocETxErrPkt:   true,
	MetricRocEOutOfOrder: true,
	MetricRocENewPktRty:  true,
}

// CounterMetrics lists metrics that are cumulative counters (use accumulation,
// not trimmed mean, during aggregation).
var CounterMetrics = map[MetricName]bool{
	MetricRXPfcPkt:       true,
	MetricRocETxErrPkt:   true,
	MetricRocEOutOfOrder: true,
	MetricRocENewPktRty:  true,
}

// IsComputeMetric reports whether m is a compute-class metric.
func IsComputeMetric(m MetricName) bool { return ComputeMetrics[m] }

// IsCommunicationMetric reports whether m is a communication-class metric.
func IsCommunicationMetric(m MetricName) bool { return CommunicationMetrics[m] }

// IsCounterMetric reports whether m is a cumulative counter.
func IsCounterMetric(m MetricName) bool { return CounterMetrics[m] }

// =============================================================================
// Metric Direction & Detection Method
// =============================================================================

// AnomalyDirection indicates whether abnormal means "too high" or "too low".
type AnomalyDirection int

const (
	DirHigh AnomalyDirection = iota // abnormally high
	DirLow                          // abnormally low
)

// DetectionMethod selects the statistical method for space-dimension detection.
type DetectionMethod string

const (
	MethodZScore   DetectionMethod = "zscore"
	MethodIQR      DetectionMethod = "iqr"
	MethodDirect   DetectionMethod = "direct"   // direct comparison (e.g. freq)
	MethodAbsolute DetectionMethod = "absolute" // > threshold → anomaly
)

// MetricMeta describes the detection parameters for a single metric.
type MetricMeta struct {
	Name         MetricName
	Category     AnomalyCategory
	Direction    AnomalyDirection
	SpaceMethod  DetectionMethod
	AbsThreshold float64 // for MethodAbsolute
}

// MetricMetaRegistry maps each metric to its meta-information.
var MetricMetaRegistry = map[MetricName]MetricMeta{
	MetricTemp:           {Name: MetricTemp, Category: CatCompute, Direction: DirHigh, SpaceMethod: MethodZScore},
	MetricPower:          {Name: MetricPower, Category: CatCompute, Direction: DirHigh, SpaceMethod: MethodZScore},
	MetricAICoreFreq:     {Name: MetricAICoreFreq, Category: CatCompute, Direction: DirLow, SpaceMethod: MethodDirect},
	MetricAICoreUtil:     {Name: MetricAICoreUtil, Category: CatCompute, Direction: DirLow, SpaceMethod: MethodZScore},
	MetricHBMUtil:        {Name: MetricHBMUtil, Category: CatCompute, Direction: DirLow, SpaceMethod: MethodZScore},
	MetricTXBandwidth:    {Name: MetricTXBandwidth, Category: CatCommunication, Direction: DirLow, SpaceMethod: MethodZScore},
	MetricRXPfcPkt:       {Name: MetricRXPfcPkt, Category: CatCommunication, Direction: DirHigh, SpaceMethod: MethodAbsolute, AbsThreshold: 0},
	MetricRocETxErrPkt:   {Name: MetricRocETxErrPkt, Category: CatCommunication, Direction: DirHigh, SpaceMethod: MethodAbsolute, AbsThreshold: 0},
	MetricRocEOutOfOrder: {Name: MetricRocEOutOfOrder, Category: CatCommunication, Direction: DirHigh, SpaceMethod: MethodAbsolute, AbsThreshold: 0},
	MetricRocENewPktRty:  {Name: MetricRocENewPktRty, Category: CatCommunication, Direction: DirHigh, SpaceMethod: MethodAbsolute, AbsThreshold: 0},
}

// =============================================================================
// Baseline
// =============================================================================

// CardBaseline holds a single card's historical distribution for one metric.
type CardBaseline struct {
	CardID int
	Metric MetricName
	Mean   float64
	StdDev float64
	P50    float64
	P95    float64
	P99    float64
	N      int
}

// =============================================================================
// Detection Results
// =============================================================================

// AnomalyCategory classifies the anomaly as compute, communication, or none.
type AnomalyCategory string

const (
	CatNone          AnomalyCategory = "none"
	CatCompute       AnomalyCategory = "compute"
	CatCommunication AnomalyCategory = "communication"
)

// Quadrant is the 2×2 time×space quadrant.
type Quadrant int

const (
	QuadNormal            Quadrant = iota // both normal
	QuadEarlyDegradation                 // time abnormal, space normal
	QuadIndividualVariance               // space abnormal, time normal
	QuadConfirmedAnomaly                 // both abnormal
)

func (q Quadrant) String() string {
	switch q {
	case QuadNormal:
		return "normal"
	case QuadEarlyDegradation:
		return "early_degradation"
	case QuadIndividualVariance:
		return "individual_variance"
	case QuadConfirmedAnomaly:
		return "confirmed_anomaly"
	default:
		return "unknown"
	}
}

// MetricAnomalyDetail records the dual-dimension anomaly scores for one metric on one card.
type MetricAnomalyDetail struct {
	Metric        MetricName `json:"metric"`
	SpaceScore    float64    `json:"space_score"`
	TimeScore     float64    `json:"time_score"`
	FusionScore   float64    `json:"fusion_score"`
	SpaceAbnormal bool       `json:"space_abnormal"`
	TimeAbnormal  bool       `json:"time_abnormal"`
	Quadrant      Quadrant   `json:"quadrant"`
	CurrentMean   float64    `json:"current_mean"`
	BaselineMean  float64    `json:"baseline_mean,omitempty"`
	PeerMean      float64    `json:"peer_mean,omitempty"`
}

// CardDetectionSummary is the per-card detection result.
type CardDetectionSummary struct {
	CardID                 int                   `json:"card_id"`
	AnomalyCategory        AnomalyCategory       `json:"anomaly_category"`
	Quadrant               Quadrant              `json:"quadrant"`
	AnomalyDetails         []MetricAnomalyDetail `json:"anomaly_details,omitempty"`
	SecondaryCommAnomalies []MetricAnomalyDetail `json:"secondary_comm_anomalies,omitempty"`
	TrendFindings          []TrendFinding        `json:"trend_findings,omitempty"`
	CompositeScore         float64               `json:"composite_score"`
	Severity               Severity              `json:"severity"`
}

// TrendFinding records a linear-trend result for one metric.
type TrendFinding struct {
	Metric   MetricName `json:"metric"`
	Slope    float64    `json:"slope"`
	RSquared float64    `json:"r_squared"`
	Desc     string     `json:"desc"`
}

// =============================================================================
// Root Cause
// =============================================================================

// RootCauseCategory enumerates diagnosed root causes.
type RootCauseCategory string

const (
	RcThermalThrottle     RootCauseCategory = "thermal_throttle"
	RcCoolingInsufficient RootCauseCategory = "cooling_insufficient"
	RcTempSensorFault     RootCauseCategory = "temp_sensor_fault"
	RcForcedDownclock     RootCauseCategory = "forced_downclock"
	RcStraggler           RootCauseCategory = "straggler"
	RcLoadImbalance       RootCauseCategory = "load_imbalance"
	RcMemBottleneck       RootCauseCategory = "memory_bottleneck"
	RcNetworkLinkIssue    RootCauseCategory = "network_link_issue"
	RcNetworkCongestion   RootCauseCategory = "network_congestion"
	RcNetworkPacketLoss   RootCauseCategory = "network_packet_loss"
	RcBandwidthLimited    RootCauseCategory = "bandwidth_limited"
	RcHardwareFault       RootCauseCategory = "hardware_fault"
	RcUnknown             RootCauseCategory = "unknown"
)

// RootCauseResult is the diagnosed root cause for one anomalous card.
type RootCauseResult struct {
	CardID     int                   `json:"card_id"`
	Category   RootCauseCategory     `json:"category"`
	Confidence Confidence            `json:"confidence"`
	Evidence   []MetricAnomalyDetail `json:"evidence"`
	Suggestion string                `json:"suggestion"`
}

// CorrelationResult records cross-card correlation findings.
type CorrelationResult struct {
	Type        string     `json:"type"` // node_level | network_level | job_level | card_level
	Description string     `json:"description"`
	CardIDs     []int      `json:"card_ids"`
	Confidence  Confidence `json:"confidence"`
}

// =============================================================================
// Severity & Confidence Enums
// =============================================================================

// Severity indicates how urgent the finding is.
type Severity string

const (
	SevCritical Severity = "critical"
	SevWarning  Severity = "warning"
	SevInfo     Severity = "info"
)

// Confidence indicates how confident the diagnosis is.
type Confidence string

const (
	ConfHigh   Confidence = "high"
	ConfMedium Confidence = "medium"
	ConfLow    Confidence = "low"
)

// =============================================================================
// Detection Config
// =============================================================================

// DetectionConfig holds all tunable parameters for KPI anomaly detection.
type DetectionConfig struct {
	// Preprocessing
	AggregationWindowSec int     // aggregation window in seconds, default 60
	TrimRatio            float64 // trimming ratio, default 0.25 (25% each side)
	MinSamplesForTrim    int     // minimum samples to apply trimming, default 4

	// Windows
	BaselineHours  float64 // historical baseline window in hours, default 360 (15 days)
	DetectionHours float64 // detection window in hours, default 1

	// Space dimension
	SpaceMethod     DetectionMethod
	SpaceZThreshold float64 // default 2.5
	SpaceIQRMult    float64 // default 1.5

	// Time dimension
	TimeZThreshold float64 // default 2.0

	// Fusion weights
	TimeWeight  float64 // α, default 0.6
	SpaceWeight float64 // β, default 0.4

	// Trend detection
	EnableTrend      bool
	TrendMinRSquared float64 // default 0.6

	// Special thresholds
	FreqDownclockGap float64 // freq downclock detection gap in MHz, default 200
	NetErrMinThresh  float64 // min threshold for network error metrics, default 0

	// Profiling integration
	FallbackToProfiling bool
	AlwaysRunProfiling  bool
}

// DefaultDetectionConfig returns a DetectionConfig with sensible defaults.
func DefaultDetectionConfig() DetectionConfig {
	return DetectionConfig{
		AggregationWindowSec: 60,
		TrimRatio:            0.25,
		MinSamplesForTrim:    4,

		BaselineHours:  360, // 15 days
		DetectionHours: 1,

		SpaceMethod:     MethodZScore,
		SpaceZThreshold: 2.5,
		SpaceIQRMult:    1.5,

		TimeZThreshold: 2.0,

		TimeWeight:  0.6,
		SpaceWeight: 0.4,

		EnableTrend:      true,
		TrendMinRSquared: 0.6,

		FreqDownclockGap: 200,
		NetErrMinThresh:  0,

		FallbackToProfiling: true,
		AlwaysRunProfiling:  false,
	}
}

// =============================================================================
// Detection Result (top-level)
// =============================================================================

// DetectionResult is the complete KPI detection output.
type DetectionResult struct {
	Summary      DetectionSummary       `json:"summary"`
	Results      []CardDetectionSummary `json:"results"`
	RootCauses   []RootCauseResult      `json:"root_causes"`
	Correlations []CorrelationResult    `json:"correlations"`
}

// DetectionSummary is the overview section of the output.
type DetectionSummary struct {
	TotalCards         int    `json:"total_cards"`
	ConfirmedAnomalies int    `json:"confirmed_anomalies"`
	EarlyDegradation   int    `json:"early_degradation"`
	IndividualVariance int    `json:"individual_variance"`
	Normal             int    `json:"normal"`
	KPICSV             string `json:"kpi_csv"`
	TotalTimePoints    int    `json:"total_time_points"`
	BaselineWindow     string `json:"baseline_window"`
	DetectionWindow    string `json:"detection_window"`
	DetectionMethod    string `json:"detection_method"`
}

// SpaceDetectionResult holds per-time-point space anomaly scores.
type SpaceDetectionResult struct {
	Scores map[int]map[MetricName][]float64
}

// TimeDetectionResult holds per-card time anomaly scores.
type TimeDetectionResult struct {
	Scores map[int]map[MetricName]float64
}

// =============================================================================
// Helpers
// =============================================================================

// MeanStd calculates the mean and standard deviation of a float64 slice.
func MeanStd(values []float64) (mean, std float64) {
	if len(values) == 0 {
		return 0, 0
	}
	n := float64(len(values))
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean = sum / n
	if len(values) < 2 {
		return mean, 0
	}
	var sqSum float64
	for _, v := range values {
		d := v - mean
		sqSum += d * d
	}
	std = math.Sqrt(sqSum / (n - 1))
	return
}

// MinMax returns the min and max of a float64 slice.
func MinMax(values []float64) (min, max float64) {
	if len(values) == 0 {
		return 0, 0
	}
	min, max = values[0], values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return
}

// Percentile calculates the p-th percentile (0..1) of sorted values.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	k := p * float64(len(sorted)-1)
	f := math.Floor(k)
	c := math.Ceil(k)
	if f == c {
		return sorted[int(k)]
	}
	return sorted[int(f)]*(c-k) + sorted[int(c)]*(k-f)
}

// HasConfirmedAnomaly reports whether any card has confirmed (dual-dimension) anomaly.
func HasConfirmedAnomaly(summaries []CardDetectionSummary) bool {
	for _, s := range summaries {
		if s.Quadrant == QuadConfirmedAnomaly {
			return true
		}
	}
	return false
}
