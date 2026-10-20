package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in export uses the same settings, probe and response builder as real leases.
// It touches synthetic checked-in media only and can run inside the packaged runtime.
func TestPodContractFixture(t *testing.T) {
	root := filepath.Join("..", "..", "fixtures", "v1")
	if destination := os.Getenv("WEAZLPOD_EXPORT_FIXTURE"); destination != "" {
		source := filepath.Join(root, "media", "aac-lc-160k-stereo.m4a")
		output := filepath.Join(destination, "media", "mp3-128-stereo.mp3")
		if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := podEncodeCommand(ctx, source, output).Run(); err != nil {
			t.Fatal(err)
		}
		rep, err := podValidate(ctx, output)
		if err != nil {
			t.Fatal(err)
		}
		_, rep.SourceHash, err = podHash(source)
		if err != nil {
			t.Fatal(err)
		}
		rep.Encoder = podEncoderVersion()
		rep.ID = "mp3-" + digest("library-fixture-weazlpod:"+rep.SourceHash+":"+rep.Encoder)
		j := podPreparation{ID: "fixture-preparation", Kind: "trackDownload", Resource: "fixture-track", State: "ready"}
		ml := mediaLease{ID: "fixture-pod-lease", Expires: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)}
		data := podReadyData(j, ml, rep, "fixture-only")
		save := func(name string, v any) {
			b, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(destination, name), append(b, '\n'), 0644); err != nil {
				t.Fatal(err)
			}
		}
		save("weazlpod-lease.json", map[string]any{"data": data})
		save("weazlpod-representation.json", rep)
		p := &podStore{encoder: rep.Encoder}
		a := &apiV1{s: &Server{}, pod: p, cpu: fixtureMediaCPUPlan()}
		save("weazlpod-capabilities.json", map[string]any{"data": map[string]any{"contractRevision": contractRevision, "weazlpod": a.podCapabilities()}})
		t.Logf("packaged sample: %d bytes, %s, %d ms, %d samples, trim %d/%d", rep.Bytes, rep.Hash, rep.Duration, rep.Samples, rep.Delay, rep.Padding)
		return
	}
	raw, err := os.ReadFile(filepath.Join(root, "weazlpod-lease.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := decodeData[map[string]any](t, raw)
	var expected podRepresentation
	raw, err = os.ReadFile(filepath.Join(root, "weazlpod-representation.json"))
	if err != nil || json.Unmarshal(raw, &expected) != nil {
		t.Fatal("representation fixture", err)
	}
	rep, err := podValidate(context.Background(), filepath.Join(root, "media", "mp3-128-stereo.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Hash != d["sha256"] || float64(rep.Bytes) != d["byteLength"] || float64(rep.Samples) != d["decodedSamples"] || rep.Hash != expected.Hash || rep.Delay != expected.Delay || rep.Padding != expected.Padding {
		t.Fatal("fixture integrity", rep, d)
	}
	if d["profile"] != podProfile || d["contentType"] != "audio/mpeg" || d["state"] != "ready" || d["supportsRanges"] != true || d["seekOrigin"] != "gapless-decoded-sample-zero" {
		t.Fatal("fixture contract drift", d)
	}
	raw, err = os.ReadFile(filepath.Join(root, "weazlpod-capabilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	capabilities := decodeData[map[string]any](t, raw)
	current := (&apiV1{s: &Server{}, pod: &podStore{encoder: "fixture"}, cpu: fixtureMediaCPUPlan()}).podCapabilities()
	b, _ := json.Marshal(current)
	c, _ := json.Marshal(capabilities["weazlpod"])
	if string(b) != string(c) || capabilities["contractRevision"] != contractRevision {
		t.Fatal("fixture capability drift", string(b), string(c))
	}
}

func fixtureMediaCPUPlan() *mediaCPUPlan {
	return &mediaCPUPlan{CPUs: []int{0, 1}, Cores: 2, ReservedCores: 2}
}
