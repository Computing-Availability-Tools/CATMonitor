package dcmi

import "testing"

// Covers the delegation methods not exercised by dcmi_test.go: device
// topology, voltage/health/error codes/card-drop/resource/sensor lookups.
func TestMockProviderExtraMethods(t *testing.T) {
	mock := &MockProvider{
		DevMax: 2,
		Volts:  map[[2]int]uint{{0, 0}: 780},
		Healths: map[[2]int]uint{{0, 0}: 0}, // 0 = healthy
		ErrorCodes:     map[[2]int]uint{{0, 0}: 0x40f84e00},
		ErrorCodeLists: map[[2]int]*DeviceErrors{{0, 0}: {Count: 2, Codes: []string{"0x40f84e00", "0x00000001"}}},
		CardDrops:      map[[2]int]bool{{0, 0}: true},
		Resources: map[[2]int]*ResourceInfo{{0, 0}: {
			PidList:    []uint{1234, 5678},
			ProcessCnt: 2,
		}},
		Sensors: map[[3]int]int{{0, 0, 1}: 42},
		NTCs:    map[[2]int][4]int{{0, 0}: {30, 31, 32, 33}},
	}
	SetProvider(mock)
	defer SetProvider(nil)

	s := Default()

	if err := s.Init(); err != nil {
		t.Errorf("Init: %v", err)
	}

	// Device topology.
	n, err := s.DeviceNumInCard(0)
	if err != nil || n != 1 {
		t.Errorf("DeviceNumInCard = %d, %v", n, err)
	}
	devMax, mcu, cpu, err := s.DeviceIDInCard(0)
	if err != nil || devMax != 2 || mcu != -1 || cpu != -1 {
		t.Errorf("DeviceIDInCard = (%d,%d,%d), %v", devMax, mcu, cpu, err)
	}

	// Voltage / health.
	v, err := s.Voltage(0, 0)
	if err != nil || v != 780 {
		t.Errorf("Voltage = %d, %v", v, err)
	}
	h, err := s.Health(0, 0)
	if err != nil || h != 0 {
		t.Errorf("Health = %d, %v", h, err)
	}

	// Error codes.
	code, err := s.ErrorCodeV2(0, 0)
	if err != nil || code != 0x40f84e00 {
		t.Errorf("ErrorCodeV2 = %#x, %v", code, err)
	}
	list, err := s.ErrorCodeList(0, 0)
	if err != nil || list.Count != 2 || len(list.Codes) != 2 || list.Codes[0] != "0x40f84e00" {
		t.Errorf("ErrorCodeList = %+v, %v", list, err)
	}

	// Card drop.
	dropped, err := s.CardDrop(0, 0)
	if err != nil || !dropped {
		t.Errorf("CardDrop = %v, %v", dropped, err)
	}

	// Resource info (process occupancy).
	res, err := s.ResourceInfo(0, 0)
	if err != nil || res.ProcessCnt != 2 || len(res.PidList) != 2 || res.PidList[0] != 1234 {
		t.Errorf("ResourceInfo = %+v, %v", res, err)
	}

	// Sensors.
	temp, err := s.SensorInfo(0, 0, 1)
	if err != nil || temp != 42 {
		t.Errorf("SensorInfo = %d, %v", temp, err)
	}
	ntc, err := s.SensorNTC(0, 0)
	if err != nil || ntc != [4]int{30, 31, 32, 33} {
		t.Errorf("SensorNTC = %v, %v", ntc, err)
	}
}

// Fields absent from the mock must degrade to errNotAvailable, not panic.
func TestMockExtraMethodsMissing(t *testing.T) {
	SetProvider(&MockProvider{})
	defer SetProvider(nil)

	s := Default()
	if _, err := s.Voltage(0, 0); err == nil {
		t.Error("Voltage unset in mock must return an error")
	}
	if _, err := s.ErrorCodeList(0, 0); err == nil {
		t.Error("ErrorCodeList unset in mock must return an error")
	}
	if _, err := s.CardDrop(0, 0); err == nil {
		t.Error("CardDrop unset in mock must return an error")
	}
	if _, err := s.ResourceInfo(0, 0); err == nil {
		t.Error("ResourceInfo unset in mock must return an error")
	}
	if _, err := s.SensorInfo(0, 0, 1); err == nil {
		t.Error("SensorInfo unset in mock must return an error")
	}
	if _, err := s.SensorNTC(0, 0); err == nil {
		t.Error("SensorNTC unset in mock must return an error")
	}
}

// Covers the nine delegation methods missed by the suites above: device
// info queries, network health, fan counters/speeds, AI CPU info, DVPP
// ratios, full PID lists, driver health and physical NPU ids.
func TestMockProviderRemainingMethods(t *testing.T) {
	mock := &MockProvider{
		DeviceInfo_: map[[4]int]uint{{0, 0, 3, 1}: 0x1234},
		NetHealths:  map[[2]int]int{{0, 0}: 1},
		FanCounts:   map[[2]int]int{{0, 0}: 4},
		FanSpeeds:   map[[3]int]int{{0, 0, 2}: 4800},
		Aicpus: map[[2]int]*AicpuInfo{{0, 0}: {
			MaxFreq:   1200,
			CurFreq:   1000,
			AicpuNum:  2,
			UtilRates: []uint{30, 60},
		}},
		DvppRatios: map[[2]int]*DvppRatio{{0, 0}: {
			VdecRatio: 10, VpcRatio: 20, VencRatio: 30,
			JpegeRatio: 40, JpegdRatio: 50,
		}},
		PidLists: map[[2]int][]uint{{0, 0}: {123, 456}},
		DriverHP: 1,
		PhyIDs:   map[[2]int]int{{0, 0}: 7},
	}
	SetProvider(mock)
	defer SetProvider(nil)

	s := Default()

	v, err := s.DeviceInfo(0, 0, 3, 1)
	if err != nil || v != 0x1234 {
		t.Errorf("DeviceInfo = %#x, %v", v, err)
	}

	nh, err := s.NetworkHealth(0, 0)
	if err != nil || nh != 1 {
		t.Errorf("NetworkHealth = %d, %v", nh, err)
	}

	fc, err := s.FanCount(0, 0)
	if err != nil || fc != 4 {
		t.Errorf("FanCount = %d, %v", fc, err)
	}

	fs, err := s.FanSpeed(0, 0, 2)
	if err != nil || fs != 4800 {
		t.Errorf("FanSpeed = %d, %v", fs, err)
	}

	ai, err := s.AicpuInfo(0, 0)
	if err != nil || ai.MaxFreq != 1200 || ai.AicpuNum != 2 || len(ai.UtilRates) != 2 || ai.UtilRates[1] != 60 {
		t.Errorf("AicpuInfo = %+v, %v", ai, err)
	}

	dr, err := s.DvppRatio(0, 0)
	if err != nil || dr.VdecRatio != 10 || dr.VencRatio != 30 || dr.JpegdRatio != 50 {
		t.Errorf("DvppRatio = %+v, %v", dr, err)
	}

	pids, err := s.ResourceInfoFull(0, 0)
	if err != nil || len(pids) != 2 || pids[0] != 123 {
		t.Errorf("ResourceInfoFull = %v, %v", pids, err)
	}

	dh, err := s.DriverHealth()
	if err != nil || dh != 1 {
		t.Errorf("DriverHealth = %d, %v", dh, err)
	}

	phy, err := s.DevicePhyID(0, 0)
	if err != nil || phy != 7 {
		t.Errorf("DevicePhyID = %d, %v", phy, err)
	}
}

// Zero-value fields of the remaining methods degrade to errors.
func TestMockRemainingMethodsMissing(t *testing.T) {
	SetProvider(&MockProvider{})
	defer SetProvider(nil)

	s := Default()
	if _, err := s.DeviceInfo(0, 0, 1, 1); err == nil {
		t.Error("DeviceInfo unset must return an error")
	}
	if _, err := s.NetworkHealth(0, 0); err == nil {
		t.Error("NetworkHealth unset must return an error")
	}
	if _, err := s.FanCount(0, 0); err == nil {
		t.Error("FanCount unset must return an error")
	}
	if _, err := s.FanSpeed(0, 0, 0); err == nil {
		t.Error("FanSpeed unset must return an error")
	}
	if _, err := s.AicpuInfo(0, 0); err == nil {
		t.Error("AicpuInfo unset must return an error")
	}
	if _, err := s.DvppRatio(0, 0); err == nil {
		t.Error("DvppRatio unset must return an error")
	}
	if _, err := s.ResourceInfoFull(0, 0); err == nil {
		t.Error("ResourceInfoFull unset must return an error")
	}
	if _, err := s.DriverHealth(); err == nil {
		t.Error("DriverHealth unset must return an error")
	}
	if _, err := s.DevicePhyID(0, 0); err == nil {
		t.Error("DevicePhyID unset must return an error")
	}
}

// Without a provider every method returns errNotAvailable (no CGo build).
func TestNotAvailableDelegation(t *testing.T) {
	SetProvider(nil)
	s := Default()
	if _, err := s.DeviceNumInCard(0); err == nil {
		t.Error("DeviceNumInCard must fail without provider")
	}
	if _, _, _, err := s.DeviceIDInCard(0); err == nil {
		t.Error("DeviceIDInCard must fail without provider")
	}
	if _, err := s.Health(0, 0); err == nil {
		t.Error("Health must fail without provider")
	}
	if _, err := s.ErrorCodeList(0, 0); err == nil {
		t.Error("ErrorCodeList must fail without provider")
	}
	if _, err := s.CardDrop(0, 0); err == nil {
		t.Error("CardDrop must fail without provider")
	}
	if _, err := s.ResourceInfo(0, 0); err == nil {
		t.Error("ResourceInfo must fail without provider")
	}
	if _, err := s.SensorInfo(0, 0, 1); err == nil {
		t.Error("SensorInfo must fail without provider")
	}
}
