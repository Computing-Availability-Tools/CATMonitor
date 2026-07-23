// Package dcmi provides a data source that calls Huawei Ascend NPU's DCMI
// (Device Control Management Interface) C library libdcmi.so via CGo.
//
// The actual CGo binding is in dcmi_cgo.go behind a `dcmi` build tag, so that
// default builds (no CANN SDK) compile cleanly: Available() returns false
// and all methods return errNotAvailable. Build with `-tags dcmi` on a real
// NPU server with CANN installed to enable the CGo binding.
//
// The source exposes a per-device typed interface. The npu collector calls
// these methods inside per-device goroutines (device-parallel collection).
// Tests inject a MockProvider via SetMock.
package dcmi

import "errors"

// errNotAvailable is returned when no DCMI provider is registered (no CGo
// binding, or libdcmi.so not loadable).
var errNotAvailable = errors.New("dcmi: not available (build with -tags dcmi on a CANN host)")

// --- Go structs mirroring the C dcmi_* structs (from dcmi_interface_api.h) ---

type ChipInfo struct {
	ChipType  string // e.g. "Ascend910A"
	ChipName  string
	ChipVer   string
	AicoreCnt uint
}

type HbmInfo struct {
	MemorySize      uint64 // MB
	Freq            uint   // MHz
	MemoryUsage     uint64 // MB
	Temp            int    // °C
	BandwidthUtilRate uint  // %
}

type EccInfo struct {
	EnableFlag            uint
	SingleBitErrorCnt     uint
	DoubleBitErrorCnt     uint
	TotalSingleBitErrorCnt uint
	TotalDoubleBitErrorCnt uint
	SingleBitIsolatedPagesCnt uint
	DoubleBitIsolatedPagesCnt uint
}

type LlcPerf struct {
	WrHitRate  uint // %
	RdHitRate  uint // %
	Throughput uint // MB/s (unit 待实测)
}

type AicpuInfo struct {
	MaxFreq   uint   // MHz
	CurFreq   uint   // MHz
	AicpuNum  uint
	UtilRates []uint // per-core %
}

type ResourceInfo struct {
	PidList    []uint // PIDs occupying the device
	ProcessCnt uint
}

// DvppRatio holds DVPP utilization ratios (from dcmi_get_device_dvpp_ratio_info).
type DvppRatio struct {
	VdecRatio  int
	VpcRatio   int
	VencRatio  int
	JpegeRatio int
	JpegdRatio int
}

// --- FetchProvider: the internal seam for CGo/mock ---

// FetchProvider mirrors the DCMI C calls, returning Go types. The CGo binding
// (dcmi_cgo.go) implements this; tests use MockProvider.
type FetchProvider interface {
	Init() error
	CardList() (cardNum int, cardList []int, err error)
	DeviceNumInCard(card int) (int, error)
	// per-device
	Temperature(card, dev int) (int, error)
	Power(card, dev int) (int, error)
	Voltage(card, dev int) (uint, error)
	Health(card, dev int) (uint, error)
	ChipInfo(card, dev int) (*ChipInfo, error)
	ErrorCodeV2(card, dev int) (uint, error)
	ResourceInfo(card, dev int) (*ResourceInfo, error)
	HbmInfo(card, dev int) (*HbmInfo, error)
	Frequency(card, dev int, freqType int) (uint, error)
	UtilizationRate(card, dev int, rateType uint) (uint, error)
	EccInfo(card, dev int, deviceType int) (*EccInfo, error)
	LlcPerf(card, dev int) (*LlcPerf, error)
	SensorInfo(card, dev int, sensorID int) (int, error)
	SensorNTC(card, dev int) ([4]int, error)
	DeviceInfo(card, dev int, mainCmd int, subCmd uint) (uint, error)
	NetworkHealth(card, dev int) (int, error)
	FanCount(card, dev int) (int, error)
	FanSpeed(card, dev int, fanID int) (int, error)
	AicpuInfo(card, dev int) (*AicpuInfo, error)
	DvppRatio(card, dev int) (*DvppRatio, error)
	ResourceInfoFull(card, dev int) ([]uint, error) // returns PID list
	// global
	DriverVersion() (string, error)
	DriverHealth() (uint, error)
}

// --- Source: what collectors call ---

type Source interface {
	Available() bool
	Init() error
	CardList() (int, []int, error)
	DeviceNumInCard(card int) (int, error)
	// Per-device DCMI queries (return errNotAvailable if no provider)
	Temperature(card, dev int) (int, error)
	Power(card, dev int) (int, error)
	Voltage(card, dev int) (uint, error)
	Health(card, dev int) (uint, error)
	ChipInfo(card, dev int) (*ChipInfo, error)
	ErrorCodeV2(card, dev int) (uint, error)
	ResourceInfo(card, dev int) (*ResourceInfo, error)
	HbmInfo(card, dev int) (*HbmInfo, error)
	Frequency(card, dev int, freqType int) (uint, error)
	UtilizationRate(card, dev int, rateType uint) (uint, error)
	EccInfo(card, dev int, deviceType int) (*EccInfo, error)
	LlcPerf(card, dev int) (*LlcPerf, error)
	SensorInfo(card, dev int, sensorID int) (int, error)
	SensorNTC(card, dev int) ([4]int, error)
	DeviceInfo(card, dev int, mainCmd int, subCmd uint) (uint, error)
	NetworkHealth(card, dev int) (int, error)
	FanCount(card, dev int) (int, error)
	FanSpeed(card, dev int, fanID int) (int, error)
	AicpuInfo(card, dev int) (*AicpuInfo, error)
	DvppRatio(card, dev int) (*DvppRatio, error)
	ResourceInfoFull(card, dev int) ([]uint, error)
	DriverVersion() (string, error)
	DriverHealth() (uint, error)
}

type defaultSource struct {
	provider FetchProvider // nil when no CGo binding
}

var defaultSrc = &defaultSource{}

func Default() Source { return defaultSrc }

func SetProvider(p FetchProvider) { defaultSrc.provider = p }

func (s *defaultSource) Available() bool { return s.provider != nil }

func (s *defaultSource) notAvail() error { return errNotAvailable }

// --- Delegation methods ---

func (s *defaultSource) Init() error {
	if s.provider == nil { return s.notAvail() }
	return s.provider.Init()
}

func (s *defaultSource) CardList() (int, []int, error) {
	if s.provider == nil { return 0, nil, s.notAvail() }
	return s.provider.CardList()
}
func (s *defaultSource) DeviceNumInCard(card int) (int, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.DeviceNumInCard(card)
}
func (s *defaultSource) Temperature(card, dev int) (int, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.Temperature(card, dev)
}
func (s *defaultSource) Power(card, dev int) (int, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.Power(card, dev)
}
func (s *defaultSource) Voltage(card, dev int) (uint, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.Voltage(card, dev)
}
func (s *defaultSource) Health(card, dev int) (uint, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.Health(card, dev)
}
func (s *defaultSource) ChipInfo(card, dev int) (*ChipInfo, error) {
	if s.provider == nil { return nil, s.notAvail() }
	return s.provider.ChipInfo(card, dev)
}
func (s *defaultSource) ErrorCodeV2(card, dev int) (uint, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.ErrorCodeV2(card, dev)
}
func (s *defaultSource) ResourceInfo(card, dev int) (*ResourceInfo, error) {
	if s.provider == nil { return nil, s.notAvail() }
	return s.provider.ResourceInfo(card, dev)
}
func (s *defaultSource) HbmInfo(card, dev int) (*HbmInfo, error) {
	if s.provider == nil { return nil, s.notAvail() }
	return s.provider.HbmInfo(card, dev)
}
func (s *defaultSource) Frequency(card, dev int, freqType int) (uint, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.Frequency(card, dev, freqType)
}
func (s *defaultSource) UtilizationRate(card, dev int, rateType uint) (uint, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.UtilizationRate(card, dev, rateType)
}
func (s *defaultSource) EccInfo(card, dev int, deviceType int) (*EccInfo, error) {
	if s.provider == nil { return nil, s.notAvail() }
	return s.provider.EccInfo(card, dev, deviceType)
}
func (s *defaultSource) LlcPerf(card, dev int) (*LlcPerf, error) {
	if s.provider == nil { return nil, s.notAvail() }
	return s.provider.LlcPerf(card, dev)
}
func (s *defaultSource) SensorInfo(card, dev int, sensorID int) (int, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.SensorInfo(card, dev, sensorID)
}
func (s *defaultSource) SensorNTC(card, dev int) ([4]int, error) {
	if s.provider == nil { return [4]int{}, s.notAvail() }
	return s.provider.SensorNTC(card, dev)
}
func (s *defaultSource) DeviceInfo(card, dev int, mainCmd int, subCmd uint) (uint, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.DeviceInfo(card, dev, mainCmd, subCmd)
}
func (s *defaultSource) NetworkHealth(card, dev int) (int, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.NetworkHealth(card, dev)
}
func (s *defaultSource) FanCount(card, dev int) (int, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.FanCount(card, dev)
}
func (s *defaultSource) FanSpeed(card, dev int, fanID int) (int, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.FanSpeed(card, dev, fanID)
}
func (s *defaultSource) AicpuInfo(card, dev int) (*AicpuInfo, error) {
	if s.provider == nil { return nil, s.notAvail() }
	return s.provider.AicpuInfo(card, dev)
}
func (s *defaultSource) DvppRatio(card, dev int) (*DvppRatio, error) {
	if s.provider == nil { return nil, s.notAvail() }
	return s.provider.DvppRatio(card, dev)
}
func (s *defaultSource) ResourceInfoFull(card, dev int) ([]uint, error) {
	if s.provider == nil { return nil, s.notAvail() }
	return s.provider.ResourceInfoFull(card, dev)
}
func (s *defaultSource) DriverVersion() (string, error) {
	if s.provider == nil { return "", s.notAvail() }
	return s.provider.DriverVersion()
}
func (s *defaultSource) DriverHealth() (uint, error) {
	if s.provider == nil { return 0, s.notAvail() }
	return s.provider.DriverHealth()
}
