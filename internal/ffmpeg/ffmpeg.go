package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// VideoQuality denotes the video quality of the video being exported
type VideoQuality string
const (
	FullHD VideoQuality = "FullHD"
	HD = "HD"
	SD = "SD"
)

const (
	FullHDHeight = 1080
	FullHDWidth = 1920
	HDHeight = 720
	HDWidth = 1280
	SDHeight = 480
	SDWidth = 854
)

// IsValid checks the type correctness
func (vq VideoQuality) IsValid() error {
	switch vq {
	case FullHD, HD, SD:
		return nil
	}
	return errors.New("unsupported video quality type")
}

// Config used for creating FFmpeg instance
type Config struct {
	FFmpegPath string
	FFprobePath string
	Destination string
}

type FFmpeg interface {
	// GetTranscodingFunc returns function which transcodes the original video file to multiple videos with adaptive bit rates
	GetTranscodingFunc(filePath string) (*TranscodingFuncResult, error)
	//ExtractThumbnail(filePath string) ExtractResult
}

type ProcessSuccess struct {
	Path string
	Quality VideoQuality
}

type ProcessFailure struct {
	Error error
	Stdout bytes.Buffer
	Stderr bytes.Buffer
	Quality VideoQuality
}

type TranscodingFuncResult struct {
	// Functions returned to be executed
	Functions []transcodingFunc
	// Info contains video detailed metadata
	Info videoInfo
	wg sync.WaitGroup
	success int32
	failure int32
	dataStream chan interface{}
	// Results contains all results from executions
	Results []interface{}
}

// Notify can passed with execution result to aggregate into Results
func (t *TranscodingFuncResult) Notify(success bool, result interface{}) {
	if t.success + t.failure >= int32(len(t.Functions)) {
		return
	}

	if success {
		atomic.AddInt32(&t.success, 1)
	} else {
		atomic.AddInt32(&t.failure, 1)
	}

	if _, ok := result.(ProcessFailure); ok {
		t.dataStream <- result
	} else if _, ok := result.(ProcessSuccess); ok {
		t.dataStream <- result
	} else {
		panic("unsupported result")
	}

	if t.success + t.failure == int32(len(t.Functions)) {
		close(t.dataStream)
	}

	t.wg.Done()
}

// GetDone returns success flag once all execution results passed via Notify
func (t *TranscodingFuncResult) GetDone(ctx context.Context) bool {
	for {
		select {
		case <- ctx.Done():
		return false
		default:
			t.wg.Wait()

			for result := range t.dataStream {
				t.Results = append(t.Results, result)
			}

			if t.failure > 0 {
				return false
			} else {
				return true
			}
		}
	}
}

type transcodingFunc struct {
	Quality VideoQuality
	Func func() ConvertResult
}

type ffmpeg struct {
	ffmpegPath string
	ffprobePath string
	destination string
}

type ExtractResult struct {
	Error error
	Stdout bytes.Buffer
	Stderr bytes.Buffer
	Stream bytes.Buffer
}

const ExtractedThumbnailWidth = 400

// TODO: extract thumbnail
//func (f ffmpeg) ExtractThumbnail(filePath string) (rs ExtractResult) {
//	hw := f.getHeightWidth(filePath)
//	if hw.Error != nil {
//		rs.Error = hw.Error
//		rs.Stderr = hw.Stderr
//		rs.Stdout = hw.Stdout
//		return
//	}
//
//	ratio := float64(hw.Height) / float64(hw.Width)
//	extractedThumbnailHeight = ExtractedThumbnailWidth * ratio
//
//}

func (f ffmpeg) GetTranscodingFunc(filePath string) (*TranscodingFuncResult, error) {
	info := f.getVideoInfo(filePath)
	if info.Error != nil {
		return nil, errors.New(
			fmt.Sprintf("unable to get video info: error= %v\n stdout= %v\n stderr= %v\n",
				info.Error,
				info.Stdout.String(),
				info.Stderr.String(),
			))
	}

	videoQualities := f.determineVideos(filePath, info)
	if videoQualities.Error != nil {
		return nil, errors.New(
			fmt.Sprintf("unable to determine videos: error= %v\n stdout= %v\n stderr= %v\n",
				videoQualities.Error,
				videoQualities.Stdout.String(),
				videoQualities.Stderr.String(),
			))
	}

	funcs := make([]transcodingFunc, 0)
	for _, quality := range videoQualities.Result {
		var tf transcodingFunc
		tf.Quality = quality
		tf.Func = f.convert(filePath, quality)
		funcs = append(funcs, tf)
	}

	var wg sync.WaitGroup
	wg.Add(len(funcs))

	return &TranscodingFuncResult{
		Functions:  funcs,
		Info: info,
		dataStream: make(chan interface{}, len(funcs)),
		wg: wg,
	}, nil
}

func filenameWithoutExtension(fn string) string {
	return strings.TrimSuffix(fn, path.Ext(fn))
}

type ConvertResult struct {
	Error error
	Stdout bytes.Buffer
	Stderr bytes.Buffer
	Result FileInfo
}

type FileInfo struct {
	Path string
	Quality VideoQuality
}

type determineResult struct {
	Error error
	Stdout bytes.Buffer
	Stderr bytes.Buffer
	Result []VideoQuality
}

type ffprobeEntriesBody struct {
	Streams []ffprobeStream `json:"streams"`
}

type ffprobeStream struct {
	Width int `json:"width"`
	Height int `json:"height"`
	Duration string `json:"duration"`
}

func (f ffmpeg) determineVideos(filePath string, info videoInfo) (rlt determineResult) {
	if info.Error != nil {
		rlt.Error = info.Error
		rlt.Stdout = info.Stdout
		rlt.Stderr = info.Stderr
		return
	}

	// note: appends video quality in ascending order
	if info.Width >= FullHDWidth {
		rlt.Result = []VideoQuality{FullHD, HD, SD}
	} else if info.Width >= HDWidth {
		rlt.Result = []VideoQuality{HD, SD}
	} else if info.Width >= SDWidth {
		rlt.Result = []VideoQuality{SD}
	} else {
		rlt.Error = errors.New("Invalid video resolution\n")
	}
	return
}

type videoInfo struct {
	Error error
	Stdout bytes.Buffer
	Stderr bytes.Buffer
	Height int
	Width int
	Duration int64
}

func (f ffmpeg) getVideoInfo(filePath string)  (h videoInfo) {
	cmd := exec.Command(f.ffprobePath, "-v", "error",
		"-select_streams", "v:0", "-show_entries", "stream=width,height,duration", "-of", "json",
		filePath)

	var stdout, stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	err := cmd.Run()
	h.Stderr = stderr
	h.Stdout = stdout

	if err != nil {
		h.Error = err
		return
	}

	var entities ffprobeEntriesBody
	err = json.Unmarshal(stdout.Bytes(), &entities)
	if err != nil {
		panic(err)
	}

	h.Height = entities.Streams[0].Height
	h.Width = entities.Streams[0].Width
	if duration, err := strconv.ParseFloat(entities.Streams[0].Duration, 64); err != nil {
		panic(err)
	} else {
		h.Duration = int64(math.Round(duration))
	}
	return
}

func (f ffmpeg) convert(filePath string, quality VideoQuality) func() ConvertResult {
	var resolution string
	var vBitrate string
	var aBitrate string

	switch quality {
	case FullHD:
		resolution = "1920x1080"
		vBitrate = "1300k"
		aBitrate = "320k"
	case HD:
		resolution = "1280x720"
		vBitrate = "1000k"
		aBitrate = "112k"
	case SD:
		resolution = "854x480"
		vBitrate = "700k"
		aBitrate = "80k"
	default:
		panic("unsupported video quality type")
	}

	return func() ConvertResult {
		destFile := path.Join(f.destination, fmt.Sprintf("%v_%v_.mp4", filenameWithoutExtension(path.Base(filePath)), resolution))
		cmd := exec.Command(f.ffmpegPath, "-y", "-i",
			filePath, "-c:v", "libx264", "-vb", vBitrate, "-s", resolution, "-c:a",
			"aac", "-ab", aBitrate,
			destFile)

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return ConvertResult{
				Error:    err,
				Stdout:    stdout,
				Stderr: stderr,
				Result: FileInfo{
					Path:    destFile,
					Quality: quality,
				},
			}
		}
		return ConvertResult{
			Error:    nil,
			Stdout:    stdout,
			Stderr: stderr,
			Result: FileInfo{
				Path:    destFile,
				Quality: quality,
			},
		}
	}
}

func NewFFmpeg(c Config) FFmpeg {
	c.FFmpegPath = getPath(c.FFmpegPath, "ffmpeg")
	c.FFprobePath = getPath(c.FFprobePath, "ffprobe")

	if c.Destination == "" {
		panic("Destination path is not specified")
	}

	c.Destination, _ = filepath.Abs(c.Destination)
	_ = os.Mkdir(c.Destination, 0755)

	return ffmpeg{
		ffmpegPath: c.FFmpegPath,
		ffprobePath: c.FFprobePath,
		destination: c.Destination,
	}
}

func getPath(execPath string, name string) string {
	if execPath == "" {
		if path, err := exec.LookPath(name); err == nil {
			return path
		} else {
			panic("FFmpeg binary path is not specified")
		}
	}
	if _, err := os.Stat(execPath); os.IsExist(err) {
		panic("FFmpeg binary is unavailable in specified path")
	}

	return execPath
}
