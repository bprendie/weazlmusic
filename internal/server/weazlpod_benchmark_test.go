package server

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
)

// Four distinct five-minute tracks, cold derivative store, full integrity checks.
// One/two workers use the same reserved CPU pool and retain the same MP3 profile.
func BenchmarkPodPlaylist(b *testing.B) {
	if mediaCPUs().Cores < 2 {
		b.Skip("requires two media cores in addition to the application reserve")
	}
	var sources [4]string
	for i := range sources {
		sources[i] = filepath.Join(b.TempDir(), fmt.Sprintf("source-%d.flac", i))
		if err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:sample_rate=48000", 440+i*220), "-t", "300", "-ac", "2", sources[i]).Run(); err != nil {
			b.Fatal(err)
		}
	}
	for _, workers := range []int{1, 2} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			for attempt := 0; attempt < b.N; attempt++ {
				b.StopTimer()
				handler, err := New(Config{DataDir: b.TempDir(), DisableWorkers: true, TranscodeWorkers: workers}, fstest.MapFS{"index.html": {Data: []byte("fixture")}})
				if err != nil {
					b.Fatal(err)
				}
				a := handler.(*Server).v1
				se := &session{User: "benchmark", ServerURL: "https://fixture.invalid", NavUser: "benchmark"}
				hashes := make([]string, len(sources))
				for i, path := range sources {
					_, hashes[i], err = podHash(path)
					if err != nil {
						b.Fatal(err)
					}
				}
				slots := make(chan struct{}, a.podWorkers())
				var wg sync.WaitGroup
				errs := make(chan error, len(sources))
				b.StartTimer()
				for i, path := range sources {
					wg.Add(1)
					go func(i int, path string) {
						defer wg.Done()
						slots <- struct{}{}
						defer func() { <-slots }()
						if _, err := a.podPublish(context.Background(), se, path, hashes[i]); err != nil {
							errs <- err
						}
					}(i, path)
				}
				wg.Wait()
				b.StopTimer()
				close(errs)
				handler.(interface{ Close() }).Close()
				for err := range errs {
					b.Fatal(err)
				}
			}
		})
	}
}
