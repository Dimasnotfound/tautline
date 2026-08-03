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

var lightpandaPathFlags = map[string]bool{
	"--ca-cert":               true,
	"--ca-path":               true,
	"--cookie":                true,
	"--cookie-jar":            true,
	"--http-cache-dir":        true,
	"--storage-sqlite-path":   true,
	"--web-bot-auth-key-file": true,
}

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "the Tautline Lightpanda shim is only required on Windows")
		os.Exit(1)
	}

	config, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Lightpanda is not installed for Tautline:", err)
		fmt.Fprintln(os.Stderr, "Run scripts\\install-lightpanda-v2.2.0.cmd first.")
		os.Exit(1)
	}

	translated, err := translatePathArguments(os.Args[1:], func(path string) (string, error) {
		arguments := []string{}
		if strings.TrimSpace(config.Distro) != "" {
			arguments = append(arguments, "-d", config.Distro)
		}
		arguments = append(arguments, "--exec", "wslpath", "-a", "-u", path)
		output, translateErr := exec.Command("wsl.exe", arguments...).CombinedOutput()
		if translateErr != nil {
			return "", fmt.Errorf("translate Windows path %s: %s", path, strings.TrimSpace(string(output)))
		}
		value := strings.TrimSpace(string(output))
		if value == "" || !strings.HasPrefix(value, "/") {
			return "", fmt.Errorf("wslpath returned an invalid path for %s", path)
		}
		return value, nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not prepare Lightpanda arguments:", err)
		os.Exit(1)
	}

	arguments := make([]string, 0, len(translated)+5)
	if strings.TrimSpace(config.Distro) != "" {
		arguments = append(arguments, "-d", config.Distro)
	}
	arguments = append(arguments, "--exec", config.Path)
	arguments = append(arguments, translated...)

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

func translatePathArguments(arguments []string, translate func(string) (string, error)) ([]string, error) {
	result := append([]string(nil), arguments...)
	for index := 0; index < len(result); index++ {
		argument := result[index]
		flagName := argument
		value := ""
		inline := false
		if separator := strings.IndexByte(argument, '='); separator > 0 {
			flagName = argument[:separator]
			value = argument[separator+1:]
			inline = true
		}
		if !lightpandaPathFlags[flagName] {
			continue
		}
		if !inline {
			if index+1 >= len(result) {
				return nil, fmt.Errorf("%s requires a path value", flagName)
			}
			index++
			value = result[index]
		}
		if !isWindowsAbsolutePath(value) {
			continue
		}
		translated, err := translate(value)
		if err != nil {
			return nil, err
		}
		if inline {
			result[index] = flagName + "=" + translated
		} else {
			result[index] = translated
		}
	}
	return result, nil
}

func isWindowsAbsolutePath(path string) bool {
	path = strings.TrimSpace(path)
	if len(path) < 3 || path[1] != ':' {
		return false
	}
	return (path[0] >= 'A' && path[0] <= 'Z' || path[0] >= 'a' && path[0] <= 'z') && (path[2] == '\\' || path[2] == '/')
}

func findConfigPath(startDirectory string) (string, error) {
	directory := filepath.Clean(startDirectory)
	for depth := 0; depth < 8; depth++ {
		candidate := filepath.Join(directory, "runtime", "v2", "config", "lightpanda-wsl.json")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return "", fmt.Errorf("could not find runtime\\v2\\config\\lightpanda-wsl.json above %s", startDirectory)
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
	configPath, err := findConfigPath(filepath.Dir(executable))
	if err != nil {
		return wslConfig{}, err
	}
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
