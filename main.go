package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

const (
	COLUMN_ID = iota
	COLUMN_INPUT
	COLUMN_OUTPUT
	COLUMN_STATUS
	COLUMN_LIMITING_ERROR
)

func getExecDir() string {
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Dir(ex)
}

func getDefaultOutputDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp"
	}
	downloads := filepath.Join(home, "Downloads")
	_, err = os.Stat(downloads)
	if err == nil {
		return downloads
	}
	desktop := filepath.Join(home, "Desktop")
	_, err = os.Stat(desktop)
	if err == nil {
		return desktop
	}
	return home
}

func resolveReferenceInput(path string, outputDir string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return path, nil
	}
	if isAudioFile(path) {
		jsonPath := defaultReferenceJSONPath(path, outputDir)
		generatedPath, err := GenerateReferenceJSON(path, detectReferenceAnalyzerPath(), jsonPath)
		if err != nil {
			return "", err
		}
		return generatedPath, nil
	}
	return "", fmt.Errorf("unsupported reference file type: %s", filepath.Ext(path))
}

func isAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".wav", ".flac", ".mp3", ".aac", ".m4a", ".ogg", ".opus":
		return true
	default:
		return false
	}
}

func createTreeViewColumn(title string, order int) *gtk.TreeViewColumn {
	renderer, _ := gtk.CellRendererTextNew()
	tvc, _ := gtk.TreeViewColumnNewWithAttribute(
		title, renderer, "text", order)
	return tvc
}

func updateListItem(model *gtk.ListStore, iter *gtk.TreeIter, m Mastering) {
	status := string(m.Status)
	if m.Status == MasteringStatusProcessing {
		status = strconv.FormatFloat(m.Progression*100, 'f', 0, 64) + "%"
	}
	model.Set(iter, []int{COLUMN_ID, COLUMN_INPUT, COLUMN_OUTPUT, COLUMN_STATUS},
		[]interface{}{m.Id, m.Input, m.Output, status})
	if m.Status == MasteringStatusSucceeded && m.LimitingError > 0 {
		model.Set(iter, []int{COLUMN_LIMITING_ERROR}, []interface{}{fmt.Sprintf("%.1f dB", m.LimitingError)})
	}
}

func main() {
	masteringRunner := CreateMasteringRunner()
	go masteringRunner.Run()
	masteringId := 0

	gtk.Init(nil)

	win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		log.Fatal("Unable to create window:", err)
	}
	win.SetTitle("phaselimiter-gui")
	win.SetDefaultSize(400, 400)
	win.Connect("destroy", func() {
		masteringRunner.Terminate()
		gtk.MainQuit()
	})

	targets, err := gtk.TargetEntryNew("text/uri-list", gtk.TARGET_OTHER_APP, 1)
	if err != nil {
		log.Fatal("Unable to create target entry:", err)
	}
	win.DragDestSet(gtk.DEST_DEFAULT_ALL, []gtk.TargetEntry{*targets}, gdk.ACTION_LINK)

	box, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 0)
	win.Add(box)

	entryLabel, err := gtk.LabelNew("Output directory")
	box.Add(entryLabel)
	entry, err := gtk.EntryNew()
	entry.SetText(getDefaultOutputDir())
	box.Add(entry)

	loudnessLabel, err := gtk.LabelNew("Target loudness")
	box.Add(loudnessLabel)
	loudness, err := gtk.SpinButtonNewWithRange(-20, 0.0, 0.01)
	loudness.SetValue(-9)
	box.Add(loudness)

	masteringLevelLabel, err := gtk.LabelNew("Mastering intensity")
	box.Add(masteringLevelLabel)
	masteringLevel, err := gtk.SpinButtonNewWithRange(0.0, 1.0, 0.01)
	masteringLevel.SetValue(1)
	box.Add(masteringLevel)

	referenceModeLabel, err := gtk.LabelNew("Target loudness mode")
	box.Add(referenceModeLabel)
	referenceMode, err := gtk.ComboBoxTextNew()
	referenceMode.AppendText("Loudness")
	referenceMode.AppendText("YouTube loudness")
	referenceMode.SetActive(0)
	box.Add(referenceMode)

	ceilingModeLabel, err := gtk.LabelNew("Ceiling mode")
	box.Add(ceilingModeLabel)
	ceilingMode, err := gtk.ComboBoxTextNew()
	ceilingMode.AppendText("Peak")
	ceilingMode.AppendText("True peak")
	ceilingMode.AppendText("True peak (15 kHz lowpass)")
	ceilingMode.SetActive(1)
	box.Add(ceilingMode)

	ceilingLabel, err := gtk.LabelNew("Ceiling (dBFS)")
	box.Add(ceilingLabel)
	ceiling, err := gtk.SpinButtonNewWithRange(-1, 0, 0.01)
	ceiling.SetValue(-0.5)
	box.Add(ceiling)

	oversamplingLabel, err := gtk.LabelNew("Oversampling")
	box.Add(oversamplingLabel)
	oversampling, err := gtk.ComboBoxTextNew()
	oversampling.AppendText("1x (Fast)")
	oversampling.AppendText("2x (Slow)")
	oversampling.SetActive(0)
	box.Add(oversampling)

	automaticMastering, err := gtk.CheckButtonNewWithLabel("Enable automatic mastering")
	automaticMastering.SetActive(true)
	box.Add(automaticMastering)

	outputFormatLabel, err := gtk.LabelNew("Output format")
	box.Add(outputFormatLabel)
	outputFormat, err := gtk.ComboBoxTextNew()
	outputFormat.AppendText("WAV (16-bit)")
	outputFormat.AppendText("WAV (24-bit)")
	outputFormat.AppendText("WAV (32-bit float)")
	outputFormat.AppendText("MP3 (320 kbps)")
	outputFormat.SetActive(2)
	box.Add(outputFormat)

	sampleRateLabel, err := gtk.LabelNew("Sample rate")
	box.Add(sampleRateLabel)
	sampleRate, err := gtk.ComboBoxTextNew()
	sampleRate.AppendText("44.1 kHz")
	sampleRate.AppendText("48 kHz")
	sampleRate.SetActive(0)
	box.Add(sampleRate)

	lowCutLabel, err := gtk.LabelNew("Low cut frequency (Hz)")
	box.Add(lowCutLabel)
	lowCut, err := gtk.SpinButtonNewWithRange(0, 40, 1)
	lowCut.SetValue(20)
	box.Add(lowCut)

	highCutLabel, err := gtk.LabelNew("High cut frequency (Hz)")
	box.Add(highCutLabel)
	highCut, err := gtk.SpinButtonNewWithRange(18000, 22000, 100)
	highCut.SetValue(20000)
	box.Add(highCut)

	algorithmLabel, err := gtk.LabelNew("Mastering algorithm")
	box.Add(algorithmLabel)
	algorithm, err := gtk.ComboBoxTextNew()
	algorithm.AppendText("v1")
	algorithm.AppendText("v2 (latest)")
	algorithm.SetActive(1)
	box.Add(algorithm)

	referenceLabel, err := gtk.LabelNew("Reference (JSON or audio, optional)")
	box.Add(referenceLabel)
	referenceInput, err := gtk.EntryNew()
	box.Add(referenceInput)
	referenceInput.DragDestSet(gtk.DEST_DEFAULT_ALL, []gtk.TargetEntry{*targets}, gdk.ACTION_LINK)
	referenceInput.Connect("drag-data-received", func(_ *gtk.Entry,
		context *gdk.DragContext,
		x, y int,
		data_ptr *gtk.SelectionData,
		info, time uint) {
		s := string(data_ptr.GetData())
		lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
		for _, line := range lines {
			fileUrl, _ := url.Parse(line)
			if line == "" || fileUrl == nil {
				continue
			}
			filePath := fileUrl.Path
			if runtime.GOOS == "windows" {
				r := regexp.MustCompile("^/([a-zA-Z]:/)")
				filePath = r.ReplaceAllString(filePath, "$1")
			}
			referenceInput.SetText(filePath)
			return
		}
	})

	bassPreservation, err := gtk.CheckButtonNewWithLabel("Preserve bass")
	box.Add(bassPreservation)

	notes, err := gtk.LabelNew(`Drop audio files.

Process
1. The input audio files are mastered
2. The output files are saved to output directory

Notes
- Same algorithm with bakuage.com/aimastering.com
- No internet access`)
	box.Add(notes)

	ls, err := gtk.ListStoreNew(glib.TYPE_INT, glib.TYPE_STRING,
		glib.TYPE_STRING, glib.TYPE_STRING, glib.TYPE_STRING)

	tv, err := gtk.TreeViewNewWithModel(ls)
	tv.AppendColumn(createTreeViewColumn("input file", COLUMN_INPUT))
	tv.AppendColumn(createTreeViewColumn("output file", COLUMN_OUTPUT))
	tv.AppendColumn(createTreeViewColumn("status", COLUMN_STATUS))
	tv.AppendColumn(createTreeViewColumn("limiter error", COLUMN_LIMITING_ERROR))
	box.Add(tv)

	var destInData = func(lbi *gtk.Window,
		context *gdk.DragContext,
		x, y int,
		data_ptr *gtk.SelectionData,
		info, time uint) {

		s := string(data_ptr.GetData())
		fmt.Println(s)
		lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")

		for _, line := range lines {
			fileUrl, _ := url.Parse(line)
			if line == "" || fileUrl == nil {
				continue
			}

			m := Mastering{}
			m.Status = MasteringStatusWaiting
			m.Id = masteringId
			masteringId += 1
			m.Ffmpeg = "ffmpeg"
			m.PhaselimiterPath = filepath.Join(getExecDir(), "phaselimiter/bin/phase_limiter")
			m.SoundQuality2Cache = filepath.Join(getExecDir(), "phaselimiter/resource/sound_quality2_cache")

			m.Input = fileUrl.Path
			if runtime.GOOS == "windows" {
				r := regexp.MustCompile("^/([a-zA-Z]:/)")
				m.Input = r.ReplaceAllString(m.Input, "$1")
			}
			outputDir, _ := entry.GetText()
			outputFormatIndex := outputFormat.GetActive()
			format := "wav"
			bitDepth := 32
			switch outputFormatIndex {
			case 0:
				bitDepth = 16
			case 1:
				bitDepth = 24
			case 3:
				format = "mp3"
			}
			m.Output = filepath.Base(m.Input)
			m.Output = strings.TrimSuffix(m.Output, filepath.Ext(m.Output))
			m.Output += "_output." + format
			m.Output = filepath.Join(outputDir, m.Output)

			m.Loudness = loudness.GetValue()
			if referenceMode.GetActive() == 1 {
				m.ReferenceMode = "youtube_loudness"
			} else {
				m.ReferenceMode = "loudness"
			}
			switch ceilingMode.GetActive() {
			case 0:
				m.CeilingMode = "peak"
			case 2:
				m.CeilingMode = "lowpass_true_peak"
			default:
				m.CeilingMode = "true_peak"
			}
			m.Ceiling = ceiling.GetValue()
			m.LimiterOversample = 1 << oversampling.GetActive()
			m.MasteringEnabled = automaticMastering.GetActive()
			m.Level = masteringLevel.GetValue()
			m.BassPreservation = bassPreservation.GetActive()
			if algorithm.GetActive() == 0 {
				m.MasteringMode = "classic"
			} else {
				m.MasteringMode = "mastering5"
			}
			referencePath, _ := referenceInput.GetText()
			if strings.TrimSpace(referencePath) != "" {
				resolvedReference, err := resolveReferenceInput(referencePath, outputDir)
				if err != nil {
					m.Status = MasteringStatusFailed
					m.Message = "failed to prepare reference JSON: " + err.Error()
					masteringRunner.Add(m)
					iter := ls.Insert(0)
					updateListItem(ls, iter, m)
					continue
				}
				m.MasteringReferenceFile = resolvedReference
				referenceInput.SetText(resolvedReference)
			}
			m.LowCutFrequency = lowCut.GetValue()
			m.HighCutFrequency = highCut.GetValue()
			m.OutputFormat = format
			m.BitDepth = bitDepth
			if sampleRate.GetActive() == 1 {
				m.SampleRate = 48000
			} else {
				m.SampleRate = 44100
			}

			masteringRunner.Add(m)

			iter := ls.Insert(0)
			updateListItem(ls, iter, m)
		}
	}
	win.Connect("drag-data-received", destInData)

	go func() {
		for {
			m := <-masteringRunner.MasteringUpdate
			fmt.Printf("%#v\n", m)

			glib.IdleAdd(func() {
				iter, _ := ls.GetIterFirst()
				if iter == nil {
					return
				}
				for {
					v, _ := ls.GetValue(iter, COLUMN_ID)
					id, _ := v.GoValue()
					if m.Id == id {
						updateListItem(ls, iter, m)
					}
					if ls.IterNext(iter) == false {
						break
					}
				}
			})
		}
	}()

	win.ShowAll()
	gtk.Main()
}
