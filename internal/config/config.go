package config

import (
	"embed"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
	"gopkg.in/yaml.v2"
)

//go:embed config.yaml
var embeddedConfig embed.FS

const (
	appDirName  = "localsend-cli"
	configFile  = "config.yaml"
	defaultPort = 53317

	EnvOutputDir  = "LOCALSEND_CLI_OUTPUT_DIR"
	EnvPort       = "LOCALSEND_CLI_PORT"
	EnvDeviceName = "LOCALSEND_CLI_DEVICE_NAME"
)

type Config struct {
	DeviceName   string `yaml:"device_name"`
	Port         int    `yaml:"port"`
	OutputDir    string `yaml:"output_dir"`
	NameOfDevice string `yaml:"-"` // resolved at runtime from DeviceName or random fallback
	Functions    struct {
		HttpFileServer  bool `yaml:"http_file_server"`
		LocalSendServer bool `yaml:"local_send_server"`
	} `yaml:"functions"`
}

var ConfigData Config

var (
	adjectives = []string{
		"Happy", "Swift", "Silent", "Clever", "Brave",
		"Gentle", "Wise", "Calm", "Lucky", "Proud",
	}
	nouns = []string{
		"Phoenix", "Wolf", "Eagle", "Lion", "Owl",
		"Shark", "Tiger", "Bear", "Hawk", "Fox",
	}
)

func generateRandomName() string {
	localRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("%s %s",
		adjectives[localRand.Intn(len(adjectives))],
		nouns[localRand.Intn(len(nouns))],
	)
}

// ConfigPath returns the path of the user's config file. Falls back to a
// hidden directory in the working directory when UserConfigDir is unavailable.
func ConfigPath() string {
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, appDirName, configFile)
	}
	return filepath.Join("."+appDirName, configFile)
}

// defaultOutputDir returns the platform-aware default for received files.
// Falls back to a relative "uploads" path so behaviour is still reasonable
// when $HOME is not resolvable (e.g. some container environments).
func defaultOutputDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Downloads", appDirName)
	}
	return "uploads"
}

func writeDefaultConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := embeddedConfig.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("read embedded config: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

func init() {
	// Layer 1: built-in defaults.
	ConfigData.Port = defaultPort
	ConfigData.OutputDir = defaultOutputDir()
	ConfigData.Functions.HttpFileServer = true
	ConfigData.Functions.LocalSendServer = true

	// Layer 2: user config file (auto-generated on first run).
	cfgPath := ConfigPath()
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := writeDefaultConfig(cfgPath); err != nil {
			logger.Debug("Failed to write default config: " + err.Error())
		} else {
			logger.Debug("Wrote default config to " + cfgPath)
		}
	}
	if data, err := os.ReadFile(cfgPath); err == nil {
		if err := yaml.Unmarshal(data, &ConfigData); err != nil {
			logger.Errorf("Failed to parse config file %s: %v", cfgPath, err)
		}
	}

	// Layer 3: environment overrides.
	if v := os.Getenv(EnvOutputDir); v != "" {
		ConfigData.OutputDir = v
	}
	if v := os.Getenv(EnvPort); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			ConfigData.Port = p
		} else {
			logger.Debugf("Ignoring invalid %s=%q: %v", EnvPort, v, err)
		}
	}
	if v := os.Getenv(EnvDeviceName); v != "" {
		ConfigData.DeviceName = v
	}

	// Resolve runtime alias. Flag overrides (handled in main) may rewrite
	// this later if --device-name is passed.
	if ConfigData.DeviceName != "" {
		ConfigData.NameOfDevice = ConfigData.DeviceName
		logger.Debug("Using configured device name: " + ConfigData.NameOfDevice)
	} else {
		ConfigData.NameOfDevice = generateRandomName()
		logger.Debug("Using randomly generated device name: " + ConfigData.NameOfDevice)
	}
}
