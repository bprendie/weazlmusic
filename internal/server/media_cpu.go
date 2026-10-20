package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const applicationCoreReserve = 2
const mediaExecFlag = "--weazl-media-exec"

type mediaCPUPlan struct {
	CPUs          []int
	Cores         int
	ReservedCores int
	Err           error
}

var mediaCPUs = sync.OnceValue(detectMediaCPUs)

// Reserve whole sibling groups when the kernel exposes physical core topology.
// A CPU quota is measured in logical CPU time, so also cap the allowed CPU count.
func planMediaCPUs(allowed []int, coreIDs map[int]string, quota int) mediaCPUPlan {
	p := mediaCPUPlan{ReservedCores: applicationCoreReserve}
	if coreIDs == nil {
		coreIDs = map[int]string{}
	}
	allowed = append([]int(nil), allowed...)
	sort.Ints(allowed)
	groups := []string{}
	seen := map[string]bool{}
	for _, cpu := range allowed {
		key := coreIDs[cpu]
		if key == "" {
			key = fmt.Sprint(cpu)
		}
		coreIDs[cpu] = key
		if !seen[key] {
			groups = append(groups, key)
			seen[key] = true
		}
	}
	if len(groups) <= applicationCoreReserve || (quota >= 0 && quota <= applicationCoreReserve) {
		p.Err = errors.New("at least two application cores must remain outside media workers")
		return p
	}
	reserved := map[string]bool{}
	for _, key := range groups[len(groups)-applicationCoreReserve:] {
		reserved[key] = true
	}
	maxCPUs := len(allowed)
	if quota >= 0 {
		maxCPUs = min(maxCPUs, quota-applicationCoreReserve)
	}
	// Prefer one hardware thread per core before adding SMT siblings.
	selected := map[int]bool{}
	selectedCores := map[string]bool{}
	for pass := 0; pass < 2; pass++ {
		for _, cpu := range allowed {
			key := coreIDs[cpu]
			if reserved[key] || selected[cpu] || len(p.CPUs) >= maxCPUs || (pass == 0 && selectedCores[key]) {
				continue
			}
			p.CPUs = append(p.CPUs, cpu)
			selected[cpu] = true
			selectedCores[key] = true
		}
	}
	sort.Ints(p.CPUs)
	p.Cores = len(selectedCores)
	return p
}
func detectMediaCPUs() mediaCPUPlan {
	var mask unix.CPUSet
	if err := unix.SchedGetaffinity(0, &mask); err != nil {
		return mediaCPUPlan{Err: err, ReservedCores: applicationCoreReserve}
	}
	allowed := []int{}
	cores := map[int]string{}
	for cpu := 0; cpu < 1024; cpu++ {
		if mask.IsSet(cpu) {
			allowed = append(allowed, cpu)
			root := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/topology/", cpu)
			core, e1 := os.ReadFile(root + "core_id")
			pkg, e2 := os.ReadFile(root + "physical_package_id")
			if e1 == nil && e2 == nil {
				cores[cpu] = strings.TrimSpace(string(pkg)) + ":" + strings.TrimSpace(string(core))
			}
		}
	}
	quota, err := mediaCPUQuota("/sys/fs/cgroup", "/proc/self/cgroup")
	if err != nil {
		return mediaCPUPlan{Err: err, ReservedCores: applicationCoreReserve}
	}
	return planMediaCPUs(allowed, cores, quota)
}
func parseCPUQuota(raw string) (int, error) {
	fields := strings.Fields(raw)
	if len(fields) != 2 {
		return 0, errors.New("invalid CPU quota")
	}
	period, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || period <= 0 {
		return 0, errors.New("invalid CPU period")
	}
	if fields[0] == "max" || fields[0] == "-1" {
		return -1, nil
	}
	quota, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || quota < 0 {
		return 0, errors.New("invalid CPU quota")
	}
	return int(quota / period), nil // Floor: fractional quota must never consume the reserve.
}
func mediaCPUQuota(root, membership string) (int, error) {
	limit := -1
	paths := []string{root}
	members, err := os.ReadFile(membership)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(members), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "0" && parts[1] == "" {
			path := filepath.Join(root, filepath.Clean("/"+parts[2]))
			for path != root && strings.HasPrefix(path, root+"/") {
				paths = append(paths, path)
				path = filepath.Dir(path)
			}
		}
		for _, controller := range strings.Split(parts[1], ",") {
			if controller == "cpu" {
				for _, mount := range []string{"cpu", "cpu,cpuacct"} {
					base := filepath.Join(root, mount)
					paths = append(paths, base)
					path := filepath.Join(base, filepath.Clean("/"+parts[2]))
					for path != base && strings.HasPrefix(path, base+"/") {
						paths = append(paths, path)
						path = filepath.Dir(path)
					}
				}
			}
		}
	}
	for _, path := range paths {
		raw, e := os.ReadFile(filepath.Join(path, "cpu.max"))
		if os.IsNotExist(e) {
			q, qe := os.ReadFile(filepath.Join(path, "cpu.cfs_quota_us"))
			period, pe := os.ReadFile(filepath.Join(path, "cpu.cfs_period_us"))
			if os.IsNotExist(qe) && os.IsNotExist(pe) {
				continue
			}
			if qe != nil || pe != nil {
				return 0, errors.New("cannot inspect CPU quota")
			}
			raw = []byte(strings.TrimSpace(string(q)) + " " + strings.TrimSpace(string(period)))
			e = nil
		}
		if e != nil {
			return 0, e
		}
		n, e := parseCPUQuota(string(raw))
		if e != nil {
			return 0, e
		}
		if n >= 0 && (limit < 0 || n < limit) {
			limit = n
		}
	}
	return limit, nil
}
func mediaCommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	binary, err := os.Executable()
	if err != nil {
		binary = "/nonexistent/weazl-media-exec"
	}
	return exec.CommandContext(ctx, binary, append([]string{mediaExecFlag, name}, args...)...)
}

// Called first by both application entrypoints and the test harness. Exec replaces
// this short-lived child, preserving PID/cancellation and all stdin/stdout pipes.
// Parent Go threads and the API process affinity are never modified.
func ExecMediaCommand() {
	if len(os.Args) < 2 || os.Args[1] != mediaExecFlag {
		return
	}
	fail := func() {
		fmt.Fprintln(os.Stderr, "media subprocess unavailable: CPU reserve or executable")
		os.Exit(126)
	}
	if len(os.Args) < 3 {
		fail()
	}
	name := os.Args[2]
	switch name {
	case "ffmpeg", "ffprobe", "fdkaac":
	default:
		fail()
	}
	plan := mediaCPUs()
	if plan.Err != nil || plan.Cores == 0 {
		fail()
	}
	path, err := exec.LookPath(name)
	if err != nil {
		fail()
	}
	runtime.LockOSThread()
	var mask unix.CPUSet
	for _, cpu := range plan.CPUs {
		mask.Set(cpu)
	}
	if unix.SchedSetaffinity(0, &mask) != nil {
		fail()
	}
	if unix.Exec(path, append([]string{name}, os.Args[3:]...), os.Environ()) != nil {
		fail()
	}
}
