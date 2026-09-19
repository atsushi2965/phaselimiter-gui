package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func resolveReferenceInput(path string, outputDir string, analyzerPath string, ffmpegPath string, soundQuality2Cache string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return path, nil
	}

	jsonPath := defaultReferenceJSONPath(path, outputDir)
	if _, err := os.Stat(jsonPath); err == nil {
		return jsonPath, nil
	}
	generatedPath, err := GenerateReferenceJSON(path, analyzerPath, ffmpegPath, soundQuality2Cache, jsonPath)
	if err != nil {
		return "", err
	}
	return generatedPath, nil
}

func defaultReferenceJSONPath(audioPath, outputDir string) string {
	base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	if outputDir == "" {
		outputDir = filepath.Dir(audioPath)
	}
	return filepath.Join(outputDir, base+".json")
}

func GenerateReferenceJSON(audioPath, analyzerPath, ffmpegPath, soundQuality2Cache, outputPath string) (string, error) {
	if strings.TrimSpace(audioPath) == "" {
		return "", fmt.Errorf("audioPath is empty")
	}
	if strings.TrimSpace(outputPath) == "" {
		outputPath = defaultReferenceJSONPath(audioPath, filepath.Dir(audioPath))
	}
	if strings.TrimSpace(analyzerPath) == "" {
		return "", fmt.Errorf("reference analyzer not found")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", err
	}

	cmd := exec.Command(
		analyzerPath,
		"--input", audioPath,
		"--ffmpeg", ffmpegPath,
		"--mode", "default",
		"--sound_quality2", "true",
		"--sound_quality2_cache", soundQuality2Cache,
		"--tmp", filepath.Join(os.TempDir(), "phaselimiter-ref"),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("reference analyzer failed: %w\noutput: %s", err, string(output))
	}
	if len(output) == 0 {
		return "", fmt.Errorf("reference analyzer returned empty output")
	}

	if err := os.WriteFile(outputPath, output, 0o644); err != nil {
		return "", err
	}
	return outputPath, nil
}
