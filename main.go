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

type AppPaths struct {
	PhaseLimiter      string
	AudioAnalyzer     string
	SoundQualityCache string
	DefaultReference  string
}

var windowsFilePathPattern = regexp.MustCompile("^/([a-zA-Z]:/)")

func newAppPaths(execDir string) AppPaths {
	phaseLimiter := filepath.Join(execDir, "phaselimiter/bin/phase_limiter")
	return AppPaths{
		PhaseLimiter:      phaseLimiter,
		AudioAnalyzer:     filepath.Join(filepath.Dir(phaseLimiter), "audio_analyzer"),
		SoundQualityCache: filepath.Join(execDir, "phaselimiter/resource/sound_quality2_cache"),
		DefaultReference:  filepath.Join(execDir, "phaselimiter/resource/mastering_reference.json"),
	}
}

func fileURIToPath(line string) (string, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", fmt.Errorf("empty file URI")
	}
	fileURL, err := url.Parse(line)
	if err != nil {
		return "", err
	}
	filePath, err := url.PathUnescape(fileURL.Path)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		filePath = windowsFilePathPattern.ReplaceAllString(filePath, "$1")
	}
	if filePath == "" {
		return "", fmt.Errorf("file URI has no path: %s", line)
	}
	return filePath, nil
}

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
	execDir := getExecDir()
	paths := newAppPaths(execDir)

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
	referenceBox, err := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 4)
	box.Add(referenceBox)
	referenceInput, err := gtk.EntryNew()
	referenceBox.Add(referenceInput)
	referenceButton, err := gtk.ButtonNewWithLabel("Browse...")
	referenceBox.Add(referenceButton)
	referenceButton.Connect("clicked", func() {
		dialog, err := gtk.FileChooserDialogNew(
			"Select reference file",
			win,
			gtk.FILE_CHOOSER_ACTION_OPEN,
			"Cancel", gtk.RESPONSE_CANCEL,
			"Open", gtk.RESPONSE_ACCEPT,
		)
		if err != nil {
			return
		}
		defer dialog.Destroy()

		jsonFilter, err := gtk.FileFilterNew()
		if err == nil {
			jsonFilter.SetName("Reference JSON (*.json)")
			jsonFilter.AddPattern("*.json")
			dialog.AddFilter(jsonFilter)
		}

		audioFilter, err := gtk.FileFilterNew()
		if err == nil {
			audioFilter.SetName("Audio files (FFmpeg)")
			audioFilter.AddPattern("*")
			dialog.AddFilter(audioFilter)
		}

		if dialog.Run() == gtk.RESPONSE_ACCEPT {
			if path, err := dialog.GetFilename(); err == nil {
				referenceInput.SetText(path)
			}
		}
	})
	referenceInput.DragDestSet(gtk.DEST_DEFAULT_ALL, []gtk.TargetEntry{*targets}, gdk.ACTION_LINK)
	referenceInput.Connect("drag-data-received", func(_ *gtk.Entry,
		context *gdk.DragContext,
		x, y int,
		data_ptr *gtk.SelectionData,
		info, time uint) {
		s := string(data_ptr.GetData())
		lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
		for _, line := range lines {
			filePath, err := fileURIToPath(line)
			if err != nil {
				continue
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
			filePath, err := fileURIToPath(line)
			if err != nil {
				continue
			}

			m := Mastering{}
			m.Status = MasteringStatusWaiting
			m.Id = masteringId
			masteringId += 1
			m.Ffmpeg = "ffmpeg"
			m.PhaselimiterPath = paths.PhaseLimiter
			m.SoundQuality2Cache = paths.SoundQualityCache

			m.Input = filePath
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
			if strings.TrimSpace(referencePath) == "" {
				referencePath = paths.DefaultReference
				referenceInput.SetText(referencePath)
			}
			m.ReferenceInput = referencePath
			m.ReferenceAnalyzerPath = paths.AudioAnalyzer
			m.ReferenceOutputDir = outputDir
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
