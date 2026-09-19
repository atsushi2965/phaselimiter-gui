package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type MasteringStatus string

const (
	MasteringStatusWaiting    = MasteringStatus("waiting")
	MasteringStatusReference  = MasteringStatus("reference")
	MasteringStatusProcessing = MasteringStatus("processing")
	MasteringStatusFailed     = MasteringStatus("failed")
	MasteringStatusSucceeded  = MasteringStatus("succeeded")
)

type Mastering struct {
	Id                     int
	Input                  string
	Output                 string
	Ffmpeg                 string
	PhaselimiterPath       string
	ReferenceInput         string
	ReferenceAnalyzerPath  string
	ReferenceOutputDir     string
	SoundQuality2Cache     string
	Loudness               float64
	ReferenceMode          string
	CeilingMode            string
	Ceiling                float64
	LimiterOversample      int
	MasteringEnabled       bool
	Level                  float64
	BassPreservation       bool
	MasteringMode          string
	MasteringReferenceFile string
	LowCutFrequency        float64
	HighCutFrequency       float64
	OutputFormat           string
	BitDepth               int
	SampleRate             int
	LimitingError          float64
	Progression            float64
	Status                 MasteringStatus
	Message                string
}
type MasteringRunner struct {
	MasteringUpdate chan Mastering
	mastering       chan Mastering
	terminated      chan bool
}

func (m Mastering) execute(update chan Mastering) {
	formatFloat := func(x float64) string {
		return strconv.FormatFloat(x, 'f', 7, 64)
	}
	formatBool := func(x bool) string {
		if x {
			return "true"
		}
		return "false"
	}

	if strings.TrimSpace(m.ReferenceInput) != "" {
		m.Status = MasteringStatusReference
	} else {
		m.Status = MasteringStatusProcessing
	}
	update <- m
	if strings.TrimSpace(m.ReferenceInput) != "" {
		resolvedReference, err := resolveReferenceInput(
			m.ReferenceInput,
			m.ReferenceOutputDir,
			m.ReferenceAnalyzerPath,
			m.Ffmpeg,
			m.SoundQuality2Cache,
			func(progress string) {
				m.Message = progress
				update <- m
			},
		)
		if err != nil {
			m.Status = MasteringStatusFailed
			m.Message = "failed to prepare reference JSON: " + err.Error()
			update <- m
			return
		}
		m.MasteringReferenceFile = resolvedReference
	}
	m.Status = MasteringStatusProcessing
	update <- m

	args := []string{
		"--input", m.Input,
		"--output", m.Output,
		"--ffmpeg", m.Ffmpeg,
		"--mastering", formatBool(m.MasteringEnabled),
		"--mastering_mode", m.MasteringMode,
		"--sound_quality2_cache", m.SoundQuality2Cache,
		"--mastering_matching_level", formatFloat(m.Level),
		"--mastering_ms_matching_level", formatFloat(m.Level),
		"--mastering5_mastering_level", formatFloat(m.Level),
		"--erb_eval_func_weighting", formatBool(m.BassPreservation),
		"--reference_mode", m.ReferenceMode,
		"--reference", formatFloat(m.Loudness),
		"--ceiling_mode", m.CeilingMode,
		"--ceiling", formatFloat(m.Ceiling),
		"--limiter_external_oversample", strconv.Itoa(m.LimiterOversample),
		"--low_cut_freq", formatFloat(m.LowCutFrequency),
		"--high_cut_freq", formatFloat(m.HighCutFrequency),
		"--output_format", m.OutputFormat,
		"--bit_depth", strconv.Itoa(m.BitDepth),
		"--sample_rate", strconv.Itoa(m.SampleRate),
	}
	if strings.TrimSpace(m.MasteringReferenceFile) != "" {
		referenceFlag := "--mastering_reference_file"
		if m.MasteringMode == "mastering5" {
			referenceFlag = "--mastering5_mastering_reference_file"
		}
		args = append(args, referenceFlag, m.MasteringReferenceFile)
	}
	cmd := exec.Command(m.PhaselimiterPath, args...)
	CmdHideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.Status = MasteringStatusFailed
		m.Message = "failed to create stdout pipe: " + err.Error()
		update <- m
		return
	}
	cmd.Stderr = cmd.Stdout

	err = cmd.Start()
	if err != nil {
		m.Status = MasteringStatusFailed
		m.Message = "failed to start command: " + err.Error()
		update <- m
		return
	}

	scanner := bufio.NewScanner(stdout)
	progressionPattern := regexp.MustCompile("progression: ([-+]?[0-9]*\\.?[0-9]+)")
	limitingErrorPattern := regexp.MustCompile("limiting_error:\\s*([-+]?[0-9]*\\.?[0-9]+)")
	//output := ""
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line)
		// output += line
		matches := progressionPattern.FindStringSubmatch(line)
		if len(matches) > 0 {
			m.Progression, _ = strconv.ParseFloat(matches[1], 64)
			update <- m
		}
		matches = limitingErrorPattern.FindStringSubmatch(line)
		if len(matches) > 0 {
			m.LimitingError, _ = strconv.ParseFloat(matches[1], 64)
			update <- m
		}
	}

	err = cmd.Wait()
	if err != nil {
		m.Status = MasteringStatusFailed
		m.Message = "command failed: " + err.Error() // + " output: " + output
		update <- m
		return
	}

	m.Progression = 1
	m.Status = MasteringStatusSucceeded
	update <- m
}

func CreateMasteringRunner() MasteringRunner {
	m := MasteringRunner{}
	m.mastering = make(chan Mastering, 1000)
	m.terminated = make(chan bool, 1000)
	m.MasteringUpdate = make(chan Mastering, 1000)
	return m
}

func (m MasteringRunner) Run() {
	for {
		select {
		case x := <-m.mastering:
			x.execute(m.MasteringUpdate)
		case _ = <-m.terminated:
			return
		}
	}
}

func (m MasteringRunner) Add(mastering Mastering) {
	m.mastering <- mastering
}

func (m MasteringRunner) Terminate() {
	m.terminated <- true
}
