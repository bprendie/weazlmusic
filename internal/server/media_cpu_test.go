package server

import (
	"context"
	"encoding/json"
	"golang.org/x/sys/unix"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestMain(m *testing.M) { ExecMediaCommand(); os.Exit(m.Run()) }
func TestMediaCPUReserve(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cpus        []int
		cores       map[int]string
		quota, want int
		ids         []int
	}{
		{"four cores", []int{0, 1, 2, 3}, nil, -1, 2, []int{0, 1}},
		{"noncontiguous affinity", []int{2, 4, 6, 8}, nil, -1, 2, []int{2, 4}},
		{"two cores unavailable", []int{0, 1}, nil, -1, 0, nil},
		{"one core unavailable", []int{3}, nil, -1, 0, nil},
		{"quota four", []int{0, 1, 2, 3, 4, 5, 6, 7}, nil, 4, 2, []int{0, 1}},
		{"quota two unavailable", []int{0, 1, 2, 3}, nil, 2, 0, nil},
		{"SMT reserve whole cores", []int{0, 1, 2, 3, 4, 5, 6, 7}, map[int]string{0: "a", 1: "b", 2: "c", 3: "d", 4: "a", 5: "b", 6: "c", 7: "d"}, -1, 2, []int{0, 1, 4, 5}},
		{"SMT quota limits CPU time", []int{0, 1, 2, 3, 4, 5, 6, 7}, map[int]string{0: "a", 1: "b", 2: "c", 3: "d", 4: "a", 5: "b", 6: "c", 7: "d"}, 4, 2, []int{0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := planMediaCPUs(tc.cpus, tc.cores, tc.quota)
			if p.Cores != tc.want || !reflect.DeepEqual(p.CPUs, tc.ids) || p.ReservedCores != 2 {
				t.Fatal(p)
			}
			if tc.want == 0 && p.Err == nil {
				t.Fatal("unsafe plan enabled")
			}
		})
	}
	for raw, want := range map[string]int{"max 100000": -1, "400000 100000": 4, "299999 100000": 2, "-1 100000": -1} {
		n, err := parseCPUQuota(raw)
		if err != nil || n != want {
			t.Fatal(raw, n, err)
		}
	}
	for _, raw := range []string{"bad", "4 0", "-2 100000", "max no"} {
		if _, err := parseCPUQuota(raw); err == nil {
			t.Fatal("bad quota", raw)
		}
	}
}
func TestMediaCPUQuotaAncestors(t *testing.T) {
	root := t.TempDir()
	membership := filepath.Join(t.TempDir(), "cgroup")
	write := func(path, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(membership, "0::/parent/child\n")
	write(filepath.Join(root, "cpu.max"), "max 100000")
	write(filepath.Join(root, "parent", "cpu.max"), "399999 100000")
	write(filepath.Join(root, "parent", "child", "cpu.max"), "600000 100000")
	if n, err := mediaCPUQuota(root, membership); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	write(membership, "2:cpu,cpuacct:/group\n")
	write(filepath.Join(root, "cpu", "group", "cpu.cfs_quota_us"), "400000")
	write(filepath.Join(root, "cpu", "group", "cpu.cfs_period_us"), "100000")
	if n, err := mediaCPUQuota(root, membership); err != nil || n != 4 {
		t.Fatal(n, err)
	}
	write(filepath.Join(root, "cpu", "group", "cpu.cfs_quota_us"), "broken")
	if _, err := mediaCPUQuota(root, membership); err == nil {
		t.Fatal("failed open on bad quota")
	}
}
func TestMediaSubprocessAffinityAndCancellation(t *testing.T) {
	plan := mediaCPUs()
	if plan.Err != nil {
		t.Fatal(plan.Err)
	}
	var before unix.CPUSet
	if err := unix.SchedGetaffinity(0, &before); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := mediaCommandContext(ctx, "ffmpeg", "-v", "error", "-re", "-f", "lavfi", "-i", "anullsrc", "-f", "null", "-")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join("/proc", itoa(int64(cmd.Process.Pid)), "comm"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(raw)) == "ffmpeg" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("launcher did not exec")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var child unix.CPUSet
	if err := unix.SchedGetaffinity(cmd.Process.Pid, &child); err != nil {
		t.Fatal(err)
	}
	for cpu := 0; cpu < 1024; cpu++ {
		want := false
		for _, id := range plan.CPUs {
			if id == cpu {
				want = true
			}
		}
		if child.IsSet(cpu) != want {
			t.Fatal("child escaped affinity", cpu, plan)
		}
	}
	var after unix.CPUSet
	if err := unix.SchedGetaffinity(0, &after); err != nil || before != after {
		t.Fatal("API affinity changed", err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled process succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel leaked FFmpeg")
	}
}
func TestMediaWorkerCapabilityLimits(t *testing.T) {
	for _, tc := range []struct{ cores, configured, want int }{{2, 0, 2}, {6, 0, 6}, {20, 0, 20}, {6, 3, 3}, {2, 8, 2}, {0, 8, 0}} {
		a := &apiV1{s: &Server{cfg: Config{TranscodeWorkers: tc.configured, TranscodeBudgetBytes: 16 << 30}}, cpu: &mediaCPUPlan{Cores: tc.cores, ReservedCores: 2}, pod: &podStore{encoder: "installed"}}
		if a.podWorkers() != tc.want {
			t.Fatal(tc, a.podWorkers())
		}
		b, _ := json.Marshal(a.podCapabilities())
		if !strings.Contains(string(b), `"reservedApplicationCores":2`) {
			t.Fatal(string(b))
		}
	}
}

func TestMediaAutomaticParallelAdmission(t *testing.T) {
	h := setup(t)
	h.login("alice")
	c := h.native("alice", "playlist")
	s := h.app.Config.Handler.(*Server)
	a := s.v1
	a.cpu = &mediaCPUPlan{Cores: 4, ReservedCores: 2}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer func() { a.cancel(); origin.Close() }()
	s.upstream.Transport = podDownloadTransport{base: s.upstream.Transport, target: origin.URL}
	for i := 0; i < 4; i++ {
		code, b, _ := h.vrequest("POST", "media/leases", map[string]string{"kind": "trackDownload", "resourceId": itoa(int64(i)) + "-0", "quality": podProfile}, c.Access, nil)
		if code != 202 {
			t.Fatal("parallel playlist preparation", i, code, string(b))
		}
	}
	code, b, headers := h.vrequest("POST", "media/leases", map[string]string{"kind": "trackDownload", "resourceId": "4-0", "quality": podProfile}, c.Access, nil)
	if code != 429 || headers.Get("Retry-After") == "" {
		t.Fatal("CPU admission limit", code, string(b))
	}
	code, b, _ = h.vrequest("GET", "capabilities", nil, c.Access, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	cap := decodeData[map[string]any](t, b)["weazlpod"].(map[string]any)
	if cap["maxWorkers"] != float64(4) || cap["reservedApplicationCores"] != float64(2) {
		t.Fatal("effective worker advertisement", cap)
	}
}

func TestMediaNoCPUHeadroom(t *testing.T) {
	if os.Getenv("WEAZL_TEST_MEDIA_DISABLED") != "1" {
		t.Skip("run in a <=2-core or <=2.99-CPU quota container")
	}
	if p := mediaCPUs(); p.Cores != 0 || p.Err == nil {
		t.Fatal("media escaped application reserve", p)
	}
	if err := mediaCommandContext(context.Background(), "ffmpeg", "-version").Run(); err == nil {
		t.Fatal("media subprocess ran without CPU headroom")
	}
	handler, err := New(Config{DataDir: t.TempDir(), DisableWorkers: true, TranscodeWorkers: 999}, fstest.MapFS{"index.html": {Data: []byte("fixture")}})
	if err != nil {
		t.Fatal(err)
	}
	defer handler.(interface{ Close() }).Close()
	a := handler.(*Server).v1
	if a.mediaEnabled || a.pod.encoder != "" || a.podWorkers() != 0 {
		t.Fatal("unavailable encoder advertised")
	}
	c := a.podCapabilities()
	if c["finiteLibrary"] != false || c["radioLive"] != false {
		t.Fatal(c)
	}
}
