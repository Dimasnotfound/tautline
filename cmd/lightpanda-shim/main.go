package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type wslConfig struct {
	Distro string `json:"distro"`
	Path   string `json:"path"`
}

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "the Tautline Lightpanda shim is only required on Windows")
		os.Exit(1)
	}

	config, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Lightpanda is not installed for Tautline:", err)
		fmt.Fprintln(os.Stderr, "Run scripts\\install-lightpanda-v2.1.0.cmd first.")
		os.Exit(1)
	}

	arguments := make([]string, 0, len(os.Args)+5)
	if strings.TrimSpace(config.Distro) != "" {
		arguments = append(arguments, "-d", config.Distro)
	}
	arguments = append(arguments, "--exec", config.Path)
	arguments = append(arguments, os.Args[1:]...)

	command := exec.Command("wsl.exe", arguments...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = append(os.Environ(), "LIGHTPANDA_DISABLE_TELEMETRY=true")
	if err := command.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "could not start Lightpanda through WSL:", err)
		os.Exit(1)
	}
}

func loadConfig() (wslConfig, error) {
	if distro := strings.TrimSpace(os.Getenv("TAUTLINE_LIGHTPANDA_WSL_DISTRO")); distro != "" {
		path := strings.TrimSpace(os.Getenv("TAUTLINE_LIGHTPANDA_WSL_PATH"))
		if path == "" {
			path = "/usr/local/bin/lightpanda"
		}
		return wslConfig{Distro: distro, Path: path}, nil
	}

	executable, err := os.Executable()
	if err != nil {
		return wslConfig{}, err
	}
	root := filepath.Dir(filepath.Dir(executable))
	configPath := filepath.Join(root, "runtime", "v2", "config", "lightpanda-wsl.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return wslConfig{}, fmt.Errorf("read %s: %w", configPath, err)
	}
	var config wslConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return wslConfig{}, fmt.Errorf("decode %s: %w", configPath, err)
	}
	config.Distro = strings.TrimSpace(config.Distro)
	config.Path = strings.TrimSpace(config.Path)
	if config.Distro == "" || config.Path == "" || !strings.HasPrefix(config.Path, "/") {
		return wslConfig{}, fmt.Errorf("invalid WSL configuration in %s", configPath)
	}
	return config, nil
}
