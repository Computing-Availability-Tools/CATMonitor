package dcmi

import "testing"

func TestNotAvailableWithoutCGo(t *testing.T) {
	// Without the `dcmi` build tag, no CGo provider is registered.
	s := Default()
	if s.Available() {
		t.Error("Available() should be false without CGo provider")
	}
	if _, err := s.Temperature(0, 0); err == nil {
		t.Error("Temperature should return error without provider")
	}
}

func TestMockProvider(t *testing.T) {
	mock := &MockProvider{
		CardListVal: []int{0},
		Temp:     map[[2]int]int{{0, 0}: 42},
		Powers:   map[[2]int]int{{0, 0}: 65},
		Healths:  map[[2]int]uint{{0, 0}: 0}, // OK
		Hbms: map[[2]int]*HbmInfo{{0, 0}: {
			MemorySize:  32768,
			MemoryUsage: 16384,
			Temp:        55,
			Freq:        1600,
		}},
		Utils: map[[3]int]uint{
			{0, 0, 2}: 45, // AICORE rate = 45%
			{0, 0, 13}: 50, // NPU rate = 50%
		},
		Freqs: map[[3]int]uint{
			{0, 0, 7}:  1800, // AICORE_CURRENT
			{0, 0, 9}:  2000, // AICORE_MAX
		},
		Eccs: map[[3]int]*EccInfo{{0, 0, 2}: { // HBM type=2
			SingleBitErrorCnt: 3,
			DoubleBitErrorCnt: 0,
		}},
		Chips: map[[2]int]*ChipInfo{{0, 0}: {
			ChipType:  "Ascend910A",
			ChipName:  "Ascend910A",
			ChipVer:   "V1",
			AicoreCnt: 32,
		}},
		DriverVer: "23.0.0",
		DriverHP:  0,
		Llcs: map[[2]int]*LlcPerf{{0, 0}: {
			WrHitRate:  85,
			RdHitRate:  90,
			Throughput: 1250,
		}},
	}
	SetProvider(mock)
	defer SetProvider(nil)

	s := Default()
	if !s.Available() {
		t.Fatal("Available() should be true with mock provider")
	}

	// Temperature
	temp, err := s.Temperature(0, 0)
	if err != nil || temp != 42 {
		t.Errorf("Temperature: expected 42, got %d err %v", temp, err)
	}

	// Power
	power, err := s.Power(0, 0)
	if err != nil || power != 65 {
		t.Errorf("Power: expected 65, got %d err %v", power, err)
	}

	// HBM info
	hbm, err := s.HbmInfo(0, 0)
	if err != nil || hbm.MemorySize != 32768 || hbm.MemoryUsage != 16384 {
		t.Errorf("HbmInfo: got %+v err %v", hbm, err)
	}

	// Utilization rate (AICORE=2)
	util, err := s.UtilizationRate(0, 0, 2)
	if err != nil || util != 45 {
		t.Errorf("UtilizationRate(AICORE): expected 45, got %d err %v", util, err)
	}

	// Frequency (AICORE_CURRENT=7)
	freq, err := s.Frequency(0, 0, 7)
	if err != nil || freq != 1800 {
		t.Errorf("Frequency(AICORE_CURRENT): expected 1800, got %d err %v", freq, err)
	}

	// ECC info (HBM type=2)
	ecc, err := s.EccInfo(0, 0, 2)
	if err != nil || ecc.SingleBitErrorCnt != 3 {
		t.Errorf("EccInfo(HBM): expected single=3, got %+v err %v", ecc, err)
	}

	// Chip info
	chip, err := s.ChipInfo(0, 0)
	if err != nil || chip.ChipType != "Ascend910A" {
		t.Errorf("ChipInfo: expected Ascend910A, got %+v err %v", chip, err)
	}

	// Driver version
	ver, err := s.DriverVersion()
	if err != nil || ver != "23.0.0" {
		t.Errorf("DriverVersion: expected 23.0.0, got %q err %v", ver, err)
	}

	// LLC perf
	llc, err := s.LlcPerf(0, 0)
	if err != nil || llc.WrHitRate != 85 || llc.Throughput != 1250 {
		t.Errorf("LlcPerf: got %+v err %v", llc, err)
	}

	// Card list
	n, cards, err := s.CardList()
	if err != nil || n != 1 || len(cards) != 1 {
		t.Errorf("CardList: expected 1 card, got n=%d cards=%v err %v", n, cards, err)
	}
}

func TestMockMissing(t *testing.T) {
	// Fields not set should return errNotAvailable.
	mock := &MockProvider{
		Temp: map[[2]int]int{{0, 0}: 42},
	}
	SetProvider(mock)
	defer SetProvider(nil)

	s := Default()
	// Temperature is set
	if temp, err := s.Temperature(0, 0); err != nil || temp != 42 {
		t.Errorf("Temperature should work, got %d err %v", temp, err)
	}
	// Power is not set
	if _, err := s.Power(0, 0); err == nil {
		t.Error("Power should return error when not set in mock")
	}
}
