//go:build cgo && linux && dcmi

// This file provides the CGo binding to libdcmi.so. It is excluded from
// default builds (behind the `dcmi` build tag) so that projects without CANN
// SDK compile cleanly. On a real NPU server, build with:
//   go build -tags dcmi ./...
// to enable DCMI metric collection.
//
// The CGo calls mirror the C function signatures in dcmi_interface_api.h.

package dcmi

/*
#cgo CFLAGS: -I/usr/local/Ascend/driver/include
#cgo LDFLAGS: -L/usr/local/Ascend/driver/lib64/driver -ldcmi
#include <stdlib.h>
#include <string.h>
// The full header is typically at /usr/local/Ascend/driver/include/dcmi_interface_api.h
#include "dcmi_interface_api.h"

// Wrapper to get char* from struct field (chip_type etc.)
static const char* dcmi_chip_type(struct dcmi_chip_info *p) { return (const char*)p->chip_type; }
static const char* dcmi_chip_name(struct dcmi_chip_info *p) { return (const char*)p->chip_name; }
static const char* dcmi_chip_ver(struct dcmi_chip_info *p) { return (const char*)p->chip_ver; }

// CGo cannot access union fields directly; use C helpers to extract values.
static int sensor_get_int(union dcmi_sensor_info *si) { return si->iint; }
static int sensor_get_ntc(union dcmi_sensor_info *si, int idx) { return si->ntc_tmp[idx]; }

// Wrapper for dcmi_get_device_errorcode_v2 — actual signature:
//   int dcmi_get_device_errorcode_v2(int card_id, int device_id,
//       int *error_count, unsigned int *error_code_list, unsigned int list_len)
// This wrapper retrieves the error count and first error code into a single uint.
static int dcmi_errorcode_v2_wrapper(int card, int dev, unsigned int *code) {
    int error_count = 0;
    unsigned int error_codes[8] = {0};
    int rc = dcmi_get_device_errorcode_v2(card, dev, &error_count, error_codes, 8);
    if (rc != 0) return rc;
    // Return the error count as the metric value (0 = no errors).
    *code = (unsigned int)error_count;
    return 0;
}

// Wrapper for dcmi_get_device_info — pass value by pointer internally.
static int dcmi_get_device_info_wrapper(int card, int dev, int main_cmd,
    unsigned int sub_cmd, unsigned int *val) {
    return dcmi_get_device_info(card, dev, main_cmd, sub_cmd, val, sizeof(unsigned int));
}
*/
import "C"

import (
	"fmt"
)

func init() {
	// Register the CGo provider so Available() returns true.
	SetProvider(&cgoProvider{})
}

type cgoProvider struct{}

func (p *cgoProvider) Init() error {
	rc := C.dcmi_init()
	if rc != 0 {
		return fmt.Errorf("dcmi_init failed: %d", int32(rc))
	}
	return nil
}

func (p *cgoProvider) CardList() (int, []int, error) {
	var cardNum C.int
	cardList := make([]C.int, 64)
	rc := C.dcmi_get_card_list(&cardNum, &cardList[0], 64)
	if rc != 0 {
		return 0, nil, fmt.Errorf("dcmi_get_card_list: %d", int32(rc))
	}
	result := make([]int, int(cardNum))
	for i := 0; i < int(cardNum); i++ {
		result[i] = int(cardList[i])
	}
	return int(cardNum), result, nil
}

func (p *cgoProvider) Temperature(card, dev int) (int, error) {
	var temp C.int
	rc := C.dcmi_get_device_temperature(C.int(card), C.int(dev), &temp)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_temperature: %d", int32(rc))
	}
	return int(temp), nil
}

func (p *cgoProvider) Power(card, dev int) (int, error) {
	var power C.int
	rc := C.dcmi_get_device_power_info(C.int(card), C.int(dev), &power)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_power_info: %d", int32(rc))
	}
	return int(power), nil
}

func (p *cgoProvider) Voltage(card, dev int) (uint, error) {
	var volt C.uint
	rc := C.dcmi_get_device_voltage(C.int(card), C.int(dev), &volt)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_voltage: %d", int32(rc))
	}
	return uint(volt), nil
}

func (p *cgoProvider) Health(card, dev int) (uint, error) {
	var health C.uint
	rc := C.dcmi_get_device_health(C.int(card), C.int(dev), &health)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_health: %d", int32(rc))
	}
	return uint(health), nil
}

func (p *cgoProvider) ChipInfo(card, dev int) (*ChipInfo, error) {
	var ci C.struct_dcmi_chip_info
	rc := C.dcmi_get_device_chip_info(C.int(card), C.int(dev), &ci)
	if rc != 0 {
		return nil, fmt.Errorf("dcmi_get_device_chip_info: %d", int32(rc))
	}
	return &ChipInfo{
		ChipType:  C.GoString(C.dcmi_chip_type(&ci)),
		ChipName:  C.GoString(C.dcmi_chip_name(&ci)),
		ChipVer:   C.GoString(C.dcmi_chip_ver(&ci)),
		AicoreCnt: uint(ci.aicore_cnt),
	}, nil
}

func (p *cgoProvider) ErrorCodeV2(card, dev int) (uint, error) {
	var code C.uint
	rc := C.dcmi_errorcode_v2_wrapper(C.int(card), C.int(dev), &code)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_errorcode_v2: %d", int32(rc))
	}
	return uint(code), nil
}

func (p *cgoProvider) ResourceInfo(card, dev int) (*ResourceInfo, error) {
	return &ResourceInfo{PidList: nil, ProcessCnt: 0}, nil
}

func (p *cgoProvider) DvppRatio(card, dev int) (*DvppRatio, error) {
	var dvpp C.struct_dcmi_dvpp_ratio_info
	rc := C.dcmi_get_device_dvpp_ratio_info(C.int(card), C.int(dev), &dvpp)
	if rc != 0 {
		return nil, fmt.Errorf("dcmi_get_device_dvpp_ratio_info: %d", int32(rc))
	}
	return &DvppRatio{
		VdecRatio:  int(dvpp.vdec_ratio),
		VpcRatio:   int(dvpp.vpc_ratio),
		VencRatio:  int(dvpp.venc_ratio),
		JpegeRatio: int(dvpp.jpege_ratio),
		JpegdRatio: int(dvpp.jpegd_ratio),
	}, nil
}

func (p *cgoProvider) ResourceInfoFull(card, dev int) ([]uint, error) {
	// dcmi_get_device_resource_info returns process PIDs.
	// Full implementation depends on CANN version struct layout.
	return nil, nil
}

func (p *cgoProvider) HbmInfo(card, dev int) (*HbmInfo, error) {
	var hbm C.struct_dcmi_hbm_info
	rc := C.dcmi_get_device_hbm_info(C.int(card), C.int(dev), &hbm)
	if rc != 0 {
		return nil, fmt.Errorf("dcmi_get_device_hbm_info: %d", int32(rc))
	}
	return &HbmInfo{
		MemorySize:      uint64(hbm.memory_size),
		Freq:            uint(hbm.freq),
		MemoryUsage:     uint64(hbm.memory_usage),
		Temp:            int(hbm.temp),
		BandwidthUtilRate: uint(hbm.bandwith_util_rate),
	}, nil
}

func (p *cgoProvider) Frequency(card, dev int, freqType int) (uint, error) {
	var freq C.uint
	rc := C.dcmi_get_device_frequency(C.int(card), C.int(dev), C.enum_dcmi_freq_type(freqType), &freq)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_frequency: %d", int32(rc))
	}
	return uint(freq), nil
}

func (p *cgoProvider) UtilizationRate(card, dev int, rateType uint) (uint, error) {
	var rate C.uint
	rc := C.dcmi_get_device_utilization_rate(C.int(card), C.int(dev), C.int(int(rateType)), &rate)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_utilization_rate: %d", int32(rc))
	}
	return uint(rate), nil
}

func (p *cgoProvider) EccInfo(card, dev int, deviceType int) (*EccInfo, error) {
	var ecc C.struct_dcmi_ecc_info
	rc := C.dcmi_get_device_ecc_info(C.int(card), C.int(dev), C.enum_dcmi_device_type(deviceType), &ecc)
	if rc != 0 {
		return nil, fmt.Errorf("dcmi_get_device_ecc_info: %d", int32(rc))
	}
	return &EccInfo{
		EnableFlag:                uint(ecc.enable_flag),
		SingleBitErrorCnt:         uint(ecc.single_bit_error_cnt),
		DoubleBitErrorCnt:         uint(ecc.double_bit_error_cnt),
		TotalSingleBitErrorCnt:    uint(ecc.total_single_bit_error_cnt),
		TotalDoubleBitErrorCnt:    uint(ecc.total_double_bit_error_cnt),
		SingleBitIsolatedPagesCnt: uint(ecc.single_bit_isolated_pages_cnt),
		DoubleBitIsolatedPagesCnt: uint(ecc.double_bit_isolated_pages_cnt),
	}, nil
}

func (p *cgoProvider) LlcPerf(card, dev int) (*LlcPerf, error) {
	var perf C.struct_dcmi_llc_perf
	rc := C.dcmi_get_device_llc_perf_para(C.int(card), C.int(dev), &perf)
	if rc != 0 {
		return nil, fmt.Errorf("dcmi_get_device_llc_perf_para: %d", int32(rc))
	}
	return &LlcPerf{
		WrHitRate:  uint(perf.wr_hit_rate),
		RdHitRate:  uint(perf.rd_hit_rate),
		Throughput: uint(perf.throughput),
	}, nil
}

func (p *cgoProvider) SensorInfo(card, dev int, sensorID int) (int, error) {
	var si C.union_dcmi_sensor_info
	rc := C.dcmi_get_device_sensor_info(C.int(card), C.int(dev), C.enum_dcmi_manager_sensor_id(sensorID), &si)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_sensor_info: %d", int32(rc))
	}
	return int(C.sensor_get_int(&si)), nil
}

func (p *cgoProvider) SensorNTC(card, dev int) ([4]int, error) {
	var si C.union_dcmi_sensor_info
	rc := C.dcmi_get_device_sensor_info(C.int(card), C.int(dev), C.DCMI_NTC_TEMP_ID, &si)
	if rc != 0 {
		return [4]int{}, fmt.Errorf("dcmi_get_device_sensor_info(NTC): %d", int32(rc))
	}
	var result [4]int
	for i := 0; i < 4; i++ {
		result[i] = int(C.sensor_get_ntc(&si, C.int(i)))
	}
	return result, nil
}

func (p *cgoProvider) DeviceInfo(card, dev int, mainCmd int, subCmd uint) (uint, error) {
	var val C.uint
	rc := C.dcmi_get_device_info_wrapper(C.int(card), C.int(dev), C.int(mainCmd), C.uint(subCmd), &val)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_info: %d", int32(rc))
	}
	return uint(val), nil
}

func (p *cgoProvider) NetworkHealth(card, dev int) (int, error) {
	var result C.enum_dcmi_rdfx_detect_result
	rc := C.dcmi_get_device_network_health(C.int(card), C.int(dev), &result)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_network_health: %d", int32(rc))
	}
	return int(result), nil
}

func (p *cgoProvider) FanCount(card, dev int) (int, error) {
	var count C.int
	rc := C.dcmi_get_device_fan_count(C.int(card), C.int(dev), &count)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_fan_count: %d", int32(rc))
	}
	return int(count), nil
}

func (p *cgoProvider) FanSpeed(card, dev int, fanID int) (int, error) {
	var speed C.int
	rc := C.dcmi_get_device_fan_speed(C.int(card), C.int(dev), C.int(fanID), &speed)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_device_fan_speed: %d", int32(rc))
	}
	return int(speed), nil
}

func (p *cgoProvider) AicpuInfo(card, dev int) (*AicpuInfo, error) {
	var ai C.struct_dcmi_aicpu_info
	rc := C.dcmi_get_device_aicpu_info(C.int(card), C.int(dev), &ai)
	if rc != 0 {
		return nil, fmt.Errorf("dcmi_get_device_aicpu_info: %d", int32(rc))
	}
	rates := make([]uint, int(ai.aicpu_num))
	for i := range rates {
		rates[i] = uint(ai.util_rate[i])
	}
	return &AicpuInfo{
		MaxFreq:   uint(ai.max_freq),
		CurFreq:   uint(ai.cur_freq),
		AicpuNum:  uint(ai.aicpu_num),
		UtilRates: rates,
	}, nil
}

func (p *cgoProvider) DriverVersion() (string, error) {
	buf := make([]C.char, 255)
	rc := C.dcmi_get_driver_version(&buf[0], 255)
	if rc != 0 {
		return "", fmt.Errorf("dcmi_get_driver_version: %d", int32(rc))
	}
	return C.GoString(&buf[0]), nil
}

func (p *cgoProvider) DriverHealth() (uint, error) {
	var health C.uint
	rc := C.dcmi_get_driver_health(&health)
	if rc != 0 {
		return 0, fmt.Errorf("dcmi_get_driver_health: %d", int32(rc))
	}
	return uint(health), nil
}
