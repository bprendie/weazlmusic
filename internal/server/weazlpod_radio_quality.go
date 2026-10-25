package server

import (
	"fmt"
	"net/http"
	"strings"
)

const podRadioQualityHeader = "X-Weazl-Radio-Quality"

func podRadioBitrate(profile string) int {
	switch profile {
	case "mp3-64":
		return 64
	case "mp3-96":
		return 96
	case podProfile:
		return 128
	}
	return 0
}

func podRadioEncoding(profile string) map[string]any {
	return map[string]any{"profile": profile, "codec": "mp3", "bitRateKbps": podRadioBitrate(profile), "bitRateMode": "cbr", "sampleRateHz": 44100, "channels": 2, "channelMode": "joint_stereo"}
}

// Select once at lease creation. Asset requests use the persisted profile only.
func podSelectRadioQuality(r *http.Request, kind, quality string) (string, error) {
	values := r.Header.Values(podRadioQualityHeader)
	if len(values) != 0 {
		if !isPod(r) || kind != "radioLive" || len(values) != 1 {
			return "", fmt.Errorf("%s requires a single value on a Weazlpod radioLive lease", podRadioQualityHeader)
		}
		profile := ""
		switch strings.ToLower(strings.TrimSpace(values[0])) {
		case "low":
			profile = "mp3-64"
		case "med":
			profile = "mp3-96"
		case "high":
			profile = podProfile
		default:
			return "", fmt.Errorf("%s must be low, med or high", podRadioQualityHeader)
		}
		if quality != "" && quality != profile {
			return "", fmt.Errorf("radio quality header and JSON quality must agree")
		}
		quality = profile
	}
	if kind == "radioLive" && isPod(r) {
		if quality == "" {
			quality = podProfile
		}
		if podRadioBitrate(quality) == 0 {
			return "", fmt.Errorf("Weazlpod radio requires mp3-64, mp3-96 or mp3-128")
		}
	}
	return quality, nil
}
