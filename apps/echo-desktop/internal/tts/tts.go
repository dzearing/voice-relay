package tts

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// Engine wraps the Piper TTS CLI binary for text-to-speech synthesis.
type Engine struct {
	piperPath  string // path to piper binary
	modelPath  string // path to .onnx voice model
	sampleRate int    // audio sample rate from .onnx.json
}

// NewEngine creates a new TTS engine with the given piper binary and model paths.
// It reads the sample rate from the .onnx.json config file.
func NewEngine(piperPath, modelPath string) *Engine {
	sampleRate := readSampleRate(modelPath + ".json")
	return &Engine{
		piperPath:  piperPath,
		modelPath:  modelPath,
		sampleRate: sampleRate,
	}
}

// Synthesize converts text to speech, returning WAV audio bytes.
func (e *Engine) Synthesize(text string) ([]byte, error) {
	cmd := exec.Command(e.piperPath,
		"--model", e.modelPath,
		"--output-raw",
	)
	cmd.Stdin = strings.NewReader(text)
	cmd.Dir = piperDir(e.piperPath)
	setSysProcAttr(cmd)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	rawPCM, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("piper failed: %w: %s", err, stderr.String())
	}

	if len(rawPCM) == 0 {
		return nil, fmt.Errorf("piper produced no audio output")
	}

	log.Printf("Piper produced %d bytes of raw PCM (rate=%d)", len(rawPCM), e.sampleRate)

	wav := pcmToWav(rawPCM, e.sampleRate, 1, 16)
	return wav, nil
}

// Close is a no-op — piper CLI processes exit immediately after synthesis.
func (e *Engine) Close() {
	// Nothing to do — no long-running process
}

// piperDir returns the directory containing the piper binary,
// used as the working directory so piper can find espeak-ng-data.
func piperDir(piperPath string) string {
	dir := piperPath
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			return dir[:i]
		}
	}
	return "."
}

// readSampleRate reads the sample rate from a piper .onnx.json config file.
// Returns 22050 as default if the file can't be read.
func readSampleRate(jsonPath string) int {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		log.Printf("Could not read voice config %s, using default sample rate 22050: %v", jsonPath, err)
		return 22050
	}

	var config struct {
		Audio struct {
			SampleRate int `json:"sample_rate"`
		} `json:"audio"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("Could not parse voice config, using default sample rate 22050: %v", err)
		return 22050
	}

	if config.Audio.SampleRate == 0 {
		return 22050
	}

	return config.Audio.SampleRate
}

// pcmToWav wraps raw PCM data in a WAV header.
func pcmToWav(pcm []byte, sampleRate, channels, bitsPerSample int) []byte {
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	dataSize := len(pcm)
	fileSize := 36 + dataSize

	buf := make([]byte, 44+dataSize)

	// RIFF header
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(fileSize))
	copy(buf[8:12], "WAVE")

	// fmt sub-chunk
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)                     // sub-chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)                      // PCM format
	binary.LittleEndian.PutUint16(buf[22:24], uint16(channels))       // channels
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))     // sample rate
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))       // byte rate
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))     // block align
	binary.LittleEndian.PutUint16(buf[34:36], uint16(bitsPerSample))  // bits per sample

	// data sub-chunk
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))
	copy(buf[44:], pcm)

	return buf
}
