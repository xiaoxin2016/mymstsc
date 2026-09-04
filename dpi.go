package main

import (
	"fmt"
	"strconv"
	"strings"
)

// scaleAuto means "follow the DPI of the monitor the window is on".
const scaleAuto = 0

// deviceScaleBuckets are the values the Remote Desktop client control accepts
// for the device scale factor. The desktop scale factor is a free percentage;
// the device scale factor is not.
var deviceScaleBuckets = []int{100, 140, 180}

// desktopScaleForDPI converts a monitor DPI into a desktop scale factor in
// percent, where 96 DPI is 100%. 120 DPI (125%) becomes 125, 144 becomes 150,
// 192 becomes 200.
func desktopScaleForDPI(dpi int) int {
	if dpi <= 0 {
		dpi = 96
	}
	return clampInt((dpi*100+48)/96, 100, 500)
}

// deviceScaleForDesktopScale picks the device scale factor that goes with a
// desktop scale factor.
//
// Only the values in deviceScaleBuckets are accepted, so the largest one that
// does not exceed the desktop scale is used. That errs towards a remote UI
// slightly smaller than the local one rather than one that overflows the
// window, and it degrades to 100 for any scale below the first bucket.
func deviceScaleForDesktopScale(desktop int) int {
	out := deviceScaleBuckets[0]
	for _, v := range deviceScaleBuckets {
		if v <= desktop {
			out = v
		}
	}
	return out
}

// parseScale parses the /scale argument: "auto", or a percentage.
func parseScale(s string) (int, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" || t == "auto" {
		return scaleAuto, nil
	}
	t = strings.TrimSuffix(t, "%")
	n, err := strconv.Atoi(t)
	if err != nil {
		return 0, fmt.Errorf("scale must be \"auto\" or a percentage, got %q", s)
	}
	if n < 100 || n > 500 {
		return 0, fmt.Errorf("scale must be between 100 and 500 percent, got %d", n)
	}
	return n, nil
}

// parseDeviceScale parses the /devicescale argument.
func parseDeviceScale(s string) (int, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" || t == "auto" {
		return scaleAuto, nil
	}
	n, err := strconv.Atoi(strings.TrimSuffix(t, "%"))
	if err != nil {
		return 0, fmt.Errorf("device scale must be \"auto\" or a percentage, got %q", s)
	}
	for _, v := range deviceScaleBuckets {
		if n == v {
			return n, nil
		}
	}
	return 0, fmt.Errorf("device scale must be one of %v, got %d", deviceScaleBuckets, n)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
