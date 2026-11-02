//go:build linux

package workloadplugin

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// HPL.dat's single-grid override is deployment-owned. No Web/CLI job may
// supply file paths or arbitrary commands. Typed per-job values are resolved
// into a private frozen input by PrepareJobParameters before this function.
func hplInput(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keys := []string{"HPL_N", "HPL_NB", "HPL_P", "HPL_Q"}
	present := 0
	for _, key := range keys {
		if env(key) != "" {
			present++
		}
	}
	if present == 0 {
		return data, nil
	}
	if present != len(keys) {
		return nil, fmt.Errorf("set all of HPL_N/HPL_NB/HPL_P/HPL_Q or none")
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) < 12 {
		return nil, fmt.Errorf("HPL.dat is incomplete")
	}
	for i, line := range []int{5, 7, 10, 11} {
		n, err := positive(keys[i])
		if err != nil {
			return nil, err
		}
		lines[line] = strconv.Itoa(n)
	}
	lines[4], lines[6], lines[9] = "1", "1", "1"
	return []byte(strings.Join(lines, "\n")), nil
}

func hplNumbers(line string, count int) ([]int64, error) {
	fields := strings.Fields(line)
	if count <= 0 || count > 10000 || len(fields) < count {
		return nil, fmt.Errorf("invalid HPL.dat dimension count")
	}
	values := make([]int64, count)
	for i := range values {
		n, err := strconv.ParseInt(fields[i], 10, 64)
		if err != nil || n <= 0 || n > 2147483647 {
			return nil, fmt.Errorf("invalid HPL.dat dimension")
		}
		values[i] = n
	}
	return values, nil
}

func validateHPLResources(data []byte, ranks, threads int, cpus float64, memory int64) error {
	if ranks <= 0 || threads <= 0 || float64(ranks)*float64(threads) > cpus {
		return fmt.Errorf("HPL ranks x threads (%d x %d) exceeds effective online CPU capacity %.2f", ranks, threads, cpus)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) < 12 {
		return fmt.Errorf("HPL.dat is incomplete")
	}
	counts := map[int]int{}
	for _, i := range []int{4, 6, 9} {
		n, err := hplNumbers(lines[i], 1)
		if err != nil {
			return err
		}
		counts[i] = int(n[0])
	}
	ns, err := hplNumbers(lines[5], counts[4])
	if err != nil {
		return err
	}
	if _, err := hplNumbers(lines[7], counts[6]); err != nil {
		return err
	}
	ps, err := hplNumbers(lines[10], counts[9])
	if err != nil {
		return err
	}
	qs, err := hplNumbers(lines[11], counts[9])
	if err != nil {
		return err
	}
	for i, p := range ps {
		if p*qs[i] != int64(ranks) {
			return fmt.Errorf("HPL P x Q must equal MPI ranks: %d x %d != %d", p, qs[i], ranks)
		}
	}
	for _, n := range ns {
		// Conservative planning margin, not a guarantee of peak allocation.
		required := 10*float64(n)*float64(n) + float64(ranks)*(64<<20)
		if memory <= 0 || required > float64(memory) {
			return fmt.Errorf("HPL N=%d estimated memory %.0f bytes exceeds available budget %d", n, required, memory)
		}
	}
	return nil
}

func cpuSet(text string) (map[int]bool, error) {
	set := map[int]bool{}
	for _, token := range strings.Split(strings.TrimSpace(text), ",") {
		parts := strings.Split(token, "-")
		if len(parts) > 2 {
			return nil, fmt.Errorf("invalid CPU list")
		}
		lo, e := strconv.Atoi(parts[0])
		if e != nil || lo < 0 {
			return nil, fmt.Errorf("invalid CPU list")
		}
		hi := lo
		if len(parts) == 2 {
			hi, e = strconv.Atoi(parts[1])
		}
		if e != nil || hi < lo || hi > 1048576 {
			return nil, fmt.Errorf("invalid CPU range")
		}
		for i := lo; i <= hi; i++ {
			set[i] = true
		}
	}
	return set, nil
}

func effectiveResources() (float64, int64, error) {
	return readResources("/proc", "/sys")
}

func readResources(proc, sys string) (float64, int64, error) {
	status, err := os.ReadFile(filepath.Join(proc, "self/status"))
	if err != nil {
		return 0, 0, err
	}
	var allowed map[int]bool
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Cpus_allowed_list:") {
			allowed, err = cpuSet(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
		}
	}
	if err != nil || len(allowed) == 0 {
		return 0, 0, fmt.Errorf("effective CPU affinity is unavailable")
	}
	data, err := os.ReadFile(filepath.Join(sys, "devices/system/cpu/online"))
	if err != nil {
		return 0, 0, err
	}
	online, err := cpuSet(string(data))
	if err != nil {
		return 0, 0, err
	}
	cpus := 0.0
	for id := range allowed {
		if online[id] {
			cpus++
		}
	}
	mem, err := os.ReadFile(filepath.Join(proc, "meminfo"))
	if err != nil {
		return 0, 0, err
	}
	var memory int64
	for _, line := range strings.Split(string(mem), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				n, _ := strconv.ParseInt(fields[1], 10, 64)
				memory = n * 1024
			}
		}
	}
	root := filepath.Join(sys, "fs/cgroup")
	paths := map[string]bool{root: true, filepath.Join(root, "cpu"): true, filepath.Join(root, "cpu,cpuacct"): true, filepath.Join(root, "memory"): true}
	membership, err := os.ReadFile(filepath.Join(proc, "self/cgroup"))
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(membership), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		bases := []string{root}
		if parts[1] != "" {
			bases = []string{filepath.Join(root, parts[1])}
			for _, c := range strings.Split(parts[1], ",") {
				bases = append(bases, filepath.Join(root, c))
			}
		}
		for _, base := range bases {
			p := filepath.Join(base, filepath.Clean("/"+parts[2]))
			for {
				paths[p] = true
				if p == base {
					break
				}
				parent := filepath.Dir(p)
				if parent == p || !strings.HasPrefix(parent, base) {
					break
				}
				p = parent
			}
		}
	}
	readInt := func(p string) int64 {
		b, e := os.ReadFile(p)
		if e != nil {
			return -1
		}
		n, e := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if e != nil {
			return -1
		}
		return n
	}
	for path := range paths {
		quota, period := readInt(filepath.Join(path, "cpu.cfs_quota_us")), readInt(filepath.Join(path, "cpu.cfs_period_us"))
		if b, e := os.ReadFile(filepath.Join(path, "cpu.max")); e == nil {
			f := strings.Fields(string(b))
			if len(f) == 2 {
				quota, _ = strconv.ParseInt(f[0], 10, 64)
				period, _ = strconv.ParseInt(f[1], 10, 64)
			}
		}
		if quota > 0 && period > 0 {
			cpus = math.Min(cpus, float64(quota)/float64(period))
		}
		for _, pair := range [][2]string{{"memory.max", "memory.current"}, {"memory.limit_in_bytes", "memory.usage_in_bytes"}} {
			limit, used := readInt(filepath.Join(path, pair[0])), readInt(filepath.Join(path, pair[1]))
			if limit > 0 && used >= 0 {
				remaining := limit - used
				if remaining < 0 {
					remaining = 0
				}
				if remaining < memory {
					memory = remaining
				}
			}
		}
	}
	if cpus <= 0 || memory <= 0 {
		return 0, 0, fmt.Errorf("no effective online CPU/memory capacity")
	}
	return cpus, memory, nil
}

func hplResourceCheck(input string, ranks, threads int) error {
	data, err := hplInput(input)
	if err != nil {
		return err
	}
	cpus, memory, err := effectiveResources()
	if err != nil {
		return err
	}
	return validateHPLResources(data, ranks, threads, cpus, memory)
}
