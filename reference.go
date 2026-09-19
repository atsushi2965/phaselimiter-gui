package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func resolveReferenceInput(path string, outputDir string, analyzerPath string, ffmpegPath string, soundQuality2Cache string, progress func(string)) (string, error) {
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
	generatedPath, err := GenerateReferenceJSON(path, analyzerPath, ffmpegPath, soundQuality2Cache, jsonPath, progress)
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

func GenerateReferenceJSON(audioPath, analyzerPath, ffmpegPath, soundQuality2Cache, outputPath string, progress func(string)) (string, error) {
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("reference analyzer failed to start: %w", err)
	}
	var stderrOutput bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		startedTasks := 0
		finishedTasks := 0
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			stderrOutput.WriteString(line)
			stderrOutput.WriteByte('\n')
			if progress == nil {
				continue
			}
			if strings.HasPrefix(line, "Task start ") {
				startedTasks++
				continue
			}
			if strings.HasPrefix(line, "Task finish ") {
				finishedTasks++
				if startedTasks > 0 {
					progress(fmt.Sprintf("reference analysis: %d%%", finishedTasks*100/startedTasks))
				}
			}
		}
	}()
	var output bytes.Buffer
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		output.WriteString(line)
		output.WriteByte('\n')
	}
	readErr := scanner.Err()
	waitErr := cmd.Wait()
	<-stderrDone
	if readErr != nil {
		return "", fmt.Errorf("read reference analyzer output: %w", readErr)
	}
	if waitErr != nil {
		return "", fmt.Errorf("reference analyzer failed: %w\nerror output: %s", waitErr, stderrOutput.String())
	}
	if output.Len() == 0 {
		return "", fmt.Errorf("reference analyzer returned empty output: %s", stderrOutput.String())
	}

	if err := os.WriteFile(outputPath, output.Bytes(), 0o644); err != nil {
		return "", err
	}
	return outputPath, nil
}
