//go:build linux

package audio

import (
	"fmt"
	"os/exec"
	"strings"
)

// Linux capture reads the default output monitor (speaker loopback, not mic).
type linuxCapture struct {
	cmd          *exec.Cmd
	stdout       *pipeReader
	sampleRate   int
	channels     int
	frameSamples int
}

func newPlatformCapture(cfg CaptureConfig) (platformCapture, error) {
	if hasCommand("pactl") && hasCommand("parec") {
		return newParecCapture(cfg)
	}
	if hasCommand("wpctl") && hasCommand("pw-record") {
		return newPWRecordCapture(cfg)
	}
	return nil, fmt.Errorf("no audio capture tools found: install pulseaudio-utils (parec/pactl) or ensure pipewire (pw-record/wpctl) is available")
}

func newParecCapture(cfg CaptureConfig) (platformCapture, error) {
	monitor, err := pulseMonitorSource()
	if err != nil {
		return nil, fmt.Errorf("find pulseaudio monitor: %w", err)
	}
	return startCaptureCommand(cfg, exec.Command(
		"parec",
		"--device="+monitor,
		"--format=s16le",
		fmt.Sprintf("--rate=%d", cfg.SampleRate),
		fmt.Sprintf("--channels=%d", cfg.Channels),
	))
}

func newPWRecordCapture(cfg CaptureConfig) (platformCapture, error) {
	monitor, err := pipeWireMonitorSource()
	if err != nil {
		return nil, fmt.Errorf("find pipewire monitor: %w", err)
	}
	return startCaptureCommand(cfg, exec.Command(
		"pw-record",
		"--target="+monitor,
		"--format", "s16",
		"--rate", fmt.Sprintf("%d", cfg.SampleRate),
		"--channels", fmt.Sprintf("%d", cfg.Channels),
		"-",
	))
}

func startCaptureCommand(cfg CaptureConfig, cmd *exec.Cmd) (platformCapture, error) {
	frameSamples := cfg.SampleRate * cfg.FrameMs / 1000 * cfg.Channels

	pr, pw, err := newPipe()
	if err != nil {
		return nil, err
	}

	cmd.Stdout = pw
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}

	_ = pw.Close()

	return &linuxCapture{
		cmd:          cmd,
		stdout:       pr,
		sampleRate:   cfg.SampleRate,
		channels:     cfg.Channels,
		frameSamples: frameSamples,
	}, nil
}

func (c *linuxCapture) read() ([]int16, error) {
	raw := make([]byte, c.frameSamples*2)
	n, err := readFull(c.stdout, raw)
	if err != nil {
		return nil, err
	}
	if n != len(raw) {
		return nil, fmt.Errorf("incomplete frame: read %d of %d bytes", n, len(raw))
	}
	return bytesToInt16LE(raw), nil
}

func (c *linuxCapture) close() error {
	if c.stdout != nil {
		_ = c.stdout.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	return nil
}

func pulseMonitorSource() (string, error) {
	out, err := exec.Command("pactl", "get-default-sink").Output()
	if err != nil {
		return "", fmt.Errorf("pactl get-default-sink: %w", err)
	}
	sink := strings.TrimSpace(string(out))
	if sink == "" {
		return "", fmt.Errorf("empty default sink")
	}
	return sink + ".monitor", nil
}

func pipeWireMonitorSource() (string, error) {
	out, err := exec.Command("wpctl", "inspect", "@DEFAULT_AUDIO_SINK@").Output()
	if err != nil {
		return "", fmt.Errorf("wpctl inspect default sink: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "node.name") {
			continue
		}
		parts := strings.Split(line, "\"")
		if len(parts) >= 2 && parts[1] != "" {
			return parts[1] + ".monitor", nil
		}
	}
	return "", fmt.Errorf("default sink monitor not found in wpctl output")
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
