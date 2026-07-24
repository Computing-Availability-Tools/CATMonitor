package dcmi

// MockProvider implements FetchProvider with canned values for testing.
// Set fields to control return values; zero-value fields return errors.
type MockProvider struct {
	Cards       int
	CardListVal []int
	Temp       map[[2]int]int
	Powers     map[[2]int]int
	Volts      map[[2]int]uint
	Healths    map[[2]int]uint
	Chips      map[[2]int]*ChipInfo
	ErrorCodes map[[2]int]uint
	Resources  map[[2]int]*ResourceInfo
	Hbms       map[[2]int]*HbmInfo
	Freqs      map[[3]int]uint      // [card,dev,freqType]
	Utils      map[[3]int]uint      // [card,dev,rateType]
	Eccs       map[[3]int]*EccInfo  // [card,dev,devType]
	Llcs       map[[2]int]*LlcPerf
	Sensors    map[[3]int]int        // [card,dev,sensorID]
	NTCs       map[[2]int][4]int
	DeviceInfo_ map[[4]int]uint     // [card,dev,mainCmd,subCmd]
	NetHealths map[[2]int]int
	FanCounts  map[[2]int]int
	FanSpeeds  map[[3]int]int        // [card,dev,fanID]
	Aicpus     map[[2]int]*AicpuInfo
	DriverVer  string
	DriverHP   uint
	DvppRatios map[[2]int]*DvppRatio
	PidLists   map[[2]int][]uint
}

func (m *MockProvider) Init() error { return nil }

func (m *MockProvider) CardList() (int, []int, error) {
	if m.CardListVal != nil {
		return len(m.CardListVal), m.CardListVal, nil
	}
	return m.Cards, nil, nil
}

func (m *MockProvider) DeviceNumInCard(card int) (int, error) {
	return 1, nil // each card has 1 device
}

func (m *MockProvider) Temperature(card, dev int) (int, error) {
	if v, ok := m.Temp[[2]int{card, dev}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) Power(card, dev int) (int, error) {
	if v, ok := m.Powers[[2]int{card, dev}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) Voltage(card, dev int) (uint, error) {
	if v, ok := m.Volts[[2]int{card, dev}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) Health(card, dev int) (uint, error) {
	if v, ok := m.Healths[[2]int{card, dev}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) ChipInfo(card, dev int) (*ChipInfo, error) {
	if v, ok := m.Chips[[2]int{card, dev}]; ok {
		return v, nil
	}
	return nil, errNotAvailable
}

func (m *MockProvider) ErrorCodeV2(card, dev int) (uint, error) {
	if v, ok := m.ErrorCodes[[2]int{card, dev}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) ResourceInfo(card, dev int) (*ResourceInfo, error) {
	if v, ok := m.Resources[[2]int{card, dev}]; ok {
		return v, nil
	}
	return nil, errNotAvailable
}

func (m *MockProvider) HbmInfo(card, dev int) (*HbmInfo, error) {
	if v, ok := m.Hbms[[2]int{card, dev}]; ok {
		return v, nil
	}
	return nil, errNotAvailable
}

func (m *MockProvider) Frequency(card, dev, freqType int) (uint, error) {
	if v, ok := m.Freqs[[3]int{card, dev, freqType}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) UtilizationRate(card, dev int, rateType uint) (uint, error) {
	if v, ok := m.Utils[[3]int{card, dev, int(rateType)}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) EccInfo(card, dev, deviceType int) (*EccInfo, error) {
	if v, ok := m.Eccs[[3]int{card, dev, deviceType}]; ok {
		return v, nil
	}
	return nil, errNotAvailable
}

func (m *MockProvider) LlcPerf(card, dev int) (*LlcPerf, error) {
	if v, ok := m.Llcs[[2]int{card, dev}]; ok {
		return v, nil
	}
	return nil, errNotAvailable
}

func (m *MockProvider) SensorInfo(card, dev, sensorID int) (int, error) {
	if v, ok := m.Sensors[[3]int{card, dev, sensorID}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) SensorNTC(card, dev int) ([4]int, error) {
	if v, ok := m.NTCs[[2]int{card, dev}]; ok {
		return v, nil
	}
	return [4]int{}, errNotAvailable
}

func (m *MockProvider) DeviceInfo(card, dev, mainCmd int, subCmd uint) (uint, error) {
	if v, ok := m.DeviceInfo_[[4]int{card, dev, mainCmd, int(subCmd)}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) NetworkHealth(card, dev int) (int, error) {
	if v, ok := m.NetHealths[[2]int{card, dev}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) FanCount(card, dev int) (int, error) {
	if v, ok := m.FanCounts[[2]int{card, dev}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) FanSpeed(card, dev, fanID int) (int, error) {
	if v, ok := m.FanSpeeds[[3]int{card, dev, fanID}]; ok {
		return v, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) AicpuInfo(card, dev int) (*AicpuInfo, error) {
	if v, ok := m.Aicpus[[2]int{card, dev}]; ok {
		return v, nil
	}
	return nil, errNotAvailable
}

func (m *MockProvider) DriverVersion() (string, error) {
	if m.DriverVer != "" {
		return m.DriverVer, nil
	}
	return "", errNotAvailable
}

func (m *MockProvider) DriverHealth() (uint, error) {
	if m.DriverHP != 0 {
		return m.DriverHP, nil
	}
	return 0, errNotAvailable
}

func (m *MockProvider) DvppRatio(card, dev int) (*DvppRatio, error) {
	if v, ok := m.DvppRatios[[2]int{card, dev}]; ok {
		return v, nil
	}
	return nil, errNotAvailable
}

func (m *MockProvider) ResourceInfoFull(card, dev int) ([]uint, error) {
	if v, ok := m.PidLists[[2]int{card, dev}]; ok {
		return v, nil
	}
	return nil, errNotAvailable
}
