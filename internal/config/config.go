package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigDir  = ".streamd"
	DefaultConfigFile = "config.yaml"
	DefaultSampleRate = 48000
	DefaultChannels   = 1
	DefaultFrameMs    = 20
)

type Config struct {
	PeerID   string         `yaml:"peer_id"`
	Firebase FirebaseConfig `yaml:"firebase"`
	Audio    AudioConfig    `yaml:"audio"`
	WebRTC   WebRTCConfig   `yaml:"webrtc"`
	LogLevel string         `yaml:"log_level"`
}

type FirebaseConfig struct {
	ProjectID       string `yaml:"project_id"`
	CredentialsFile string `yaml:"credentials_file"`
}

type AudioConfig struct {
	SampleRate    int    `yaml:"sample_rate"`
	Channels      int    `yaml:"channels"`
	FrameMs       int    `yaml:"frame_ms"`
	CaptureDevice string `yaml:"capture_device"` // macOS: e.g. "BlackHole 2ch"
}

type WebRTCConfig struct {
	STUNServers []string `yaml:"stun_servers"`
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, DefaultConfigDir), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DefaultConfigFile), nil
}

func PIDPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "streamd.pid"), nil
}

func LogPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "streamd.log"), nil
}

func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if cfg.Firebase.CredentialsFile != "" {
		cfg.Firebase.CredentialsFile = expandHome(cfg.Firebase.CredentialsFile)
	}

	return cfg, nil
}

func Default() *Config {
	return &Config{
		Audio: AudioConfig{
			SampleRate: DefaultSampleRate,
			Channels:   DefaultChannels,
			FrameMs:    DefaultFrameMs,
		},
		WebRTC: WebRTCConfig{
			STUNServers: []string{"stun:stun.l.google.com:19302"},
		},
		LogLevel: "info",
	}
}

func (c *Config) Validate() error {
	if c.PeerID == "" {
		return fmt.Errorf("peer_id is required")
	}
	if c.Firebase.ProjectID == "" {
		return fmt.Errorf("firebase.project_id is required")
	}
	if c.Firebase.CredentialsFile == "" {
		return fmt.Errorf("firebase.credentials_file is required")
	}
	if c.Audio.SampleRate <= 0 {
		c.Audio.SampleRate = DefaultSampleRate
	}
	if c.Audio.Channels <= 0 {
		c.Audio.Channels = DefaultChannels
	}
	if c.Audio.FrameMs <= 0 {
		c.Audio.FrameMs = DefaultFrameMs
	}
	if len(c.WebRTC.STUNServers) == 0 {
		c.WebRTC.STUNServers = []string{"stun:stun.l.google.com:19302"}
	}
	return nil
}

func (c *Config) FrameSamples() int {
	return c.Audio.SampleRate * c.Audio.FrameMs / 1000
}

func expandHome(path string) string {
	if len(path) >= 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	return path
}

func EnsureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}
