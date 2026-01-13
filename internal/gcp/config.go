package gcp

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/kedare/compass/internal/logger"
)

func getDefaultProject() string {
	envVars := []string{
		"CLOUDSDK_CORE_PROJECT",
		"GOOGLE_CLOUD_PROJECT",
		"GCLOUD_PROJECT",
		"GCP_PROJECT",
	}

	for _, key := range envVars {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			logger.Log.Debugf("Using default project from %s", key)

			return value
		}
	}

	project, err := readProjectFromGCloudConfig()
	if err == nil && project != "" {
		logger.Log.Debug("Using default project from gcloud configuration")

		return project
	}

	return ""
}

func readProjectFromGCloudConfig() (string, error) {
	configDir := os.Getenv("CLOUDSDK_CONFIG")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}

		configDir = filepath.Join(home, ".config", "gcloud")
	}

	configPath := filepath.Join(configDir, "configurations", "config_default")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	inCore := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.Trim(line, "[]")
			inCore = strings.EqualFold(section, "core")

			continue
		}

		if inCore && strings.HasPrefix(line, "project") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", errors.New("project not found in gcloud config")
}
