package server

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
)

// Use the ipod.py AAC-LC/44100/stereo profile with the user-requested 128 kbps rate.
const recorderBitrateKbps = 128

// ADTS is only the pipe transport; published files are seekable faststart M4A.
type recorderEncoder struct {
	Commands []*exec.Cmd
	Input    io.WriteCloser
	once     sync.Once
}

func startRecorderEncoder(dir string, seconds int) (*recorderEncoder, error) {
	pcmRead, pcmWrite, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	defer pcmRead.Close()
	defer pcmWrite.Close()
	aacRead, aacWrite, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	defer aacRead.Close()
	defer aacWrite.Close()
	decode := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "pipe,file", "-probesize", "32768", "-analyzeduration", "1000000", "-i", "pipe:0", "-map", "0:a:0", "-vn", "-threads", "1", "-f", "wav", "-ar", "44100", "-ac", "2", "-sample_fmt", "s16", "pipe:1")
	encode := exec.Command("fdkaac", "-S", "-I", "-p", "2", "-b", strconv.Itoa(recorderBitrateKbps)+"k", "-f", "2", "-o", "-", "-")
	pack := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "pipe,file", "-f", "aac", "-probesize", "32768", "-analyzeduration", "1000000", "-i", "pipe:0", "-map", "0:a:0", "-c:a", "copy", "-bsf:a", "aac_adtstoasc", "-f", "segment", "-segment_time", strconv.Itoa(seconds), "-reset_timestamps", "1", "-segment_format", "mp4", "-segment_format_options", "movflags=+faststart", "-segment_list", filepath.Join(dir, "segments.csv"), "-segment_list_type", "csv", filepath.Join(dir, "%06d.m4a"))
	decode.Stdout = pcmWrite
	encode.Stdin = pcmRead
	encode.Stdout = aacWrite
	pack.Stdin = aacRead
	input, e := decode.StdinPipe()
	if e != nil {
		return nil, e
	}
	pipeline := &recorderEncoder{Commands: []*exec.Cmd{decode, encode, pack}, Input: input}
	started := []*exec.Cmd{}
	for _, command := range []*exec.Cmd{pack, encode, decode} {
		if e = command.Start(); e != nil {
			input.Close()
			for _, cmd := range started {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
			return nil, e
		}
		started = append(started, command)
	}
	return pipeline, nil
}
func (p *recorderEncoder) wait() error {
	var first error
	for _, cmd := range p.Commands {
		if e := cmd.Wait(); e != nil && first == nil {
			first = e
		}
	}
	return first
}
func (p *recorderEncoder) kill() {
	p.once.Do(func() {
		for _, cmd := range p.Commands {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	})
}
