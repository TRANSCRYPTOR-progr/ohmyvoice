package feedback

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

var (
	winmm          = syscall.NewLazyDLL("winmm.dll")
	procPlaySoundW = winmm.NewProc("PlaySoundW")
)

const (
	SND_ASYNC     = 0x0001
	SND_NODEFAULT = 0x0002
	SND_MEMORY    = 0x0004
)

var (
	mu sync.Mutex

	startSoundWAV []byte
	stopSoundWAV  []byte
	doneSoundWAV  []byte

	fallbackStart []byte
	fallbackStop  []byte
	fallbackDone  []byte
)

func init() {
	// Synthesize soft, modern UI audio chimes as initial baseline
	fallbackStart = generateGlide(580, 920, 0.08, 0.32)
	fallbackStop = generateGlide(820, 480, 0.07, 0.28)
	fallbackDone = generateSuccessChime()

	startSoundWAV = fallbackStart
	stopSoundWAV = fallbackStop
	doneSoundWAV = fallbackDone
}

// Init loads WAV sound files from the sounds/ directory directly into memory
func Init(baseDir string) {
	mu.Lock()
	defer mu.Unlock()

	soundsDir := filepath.Join(baseDir, "sounds")

	loadWAV := func(name string, fallback []byte) []byte {
		wavPath := filepath.Join(soundsDir, name+".wav")
		data, err := os.ReadFile(wavPath)
		if err == nil && len(data) > 44 {
			return data
		}
		return fallback
	}

	startSoundWAV = loadWAV("start", fallbackStart)
	stopSoundWAV = loadWAV("stop", fallbackStop)
	doneSoundWAV = loadWAV("done", fallbackDone)
}

func Close() {
	// No persistent OS handles to close for in-memory WAVs
}

// PlayStartBeep plays the start recording sound (Discord PTT on)
func PlayStartBeep() {
	mu.Lock()
	data := startSoundWAV
	mu.Unlock()

	if len(data) > 0 {
		procPlaySoundW.Call(
			uintptr(unsafe.Pointer(&data[0])),
			0,
			SND_MEMORY|SND_ASYNC|SND_NODEFAULT,
		)
	}
}

// PlayStopBeep plays the stop recording sound (Discord PTT off)
func PlayStopBeep() {
	mu.Lock()
	data := stopSoundWAV
	mu.Unlock()

	if len(data) > 0 {
		procPlaySoundW.Call(
			uintptr(unsafe.Pointer(&data[0])),
			0,
			SND_MEMORY|SND_ASYNC|SND_NODEFAULT,
		)
	}
}

// PlayDoneBeep plays the transcription success sound (Discord message)
func PlayDoneBeep() {
	mu.Lock()
	data := doneSoundWAV
	mu.Unlock()

	if len(data) > 0 {
		procPlaySoundW.Call(
			uintptr(unsafe.Pointer(&data[0])),
			0,
			SND_MEMORY|SND_ASYNC|SND_NODEFAULT,
		)
	}
}

// generateGlide creates a smooth pitch glide chime with natural exponential decay
func generateGlide(startFreq, endFreq float64, durationSec float64, volume float64) []byte {
	sampleRate := 44100
	numSamples := int(float64(sampleRate) * durationSec)
	samples := make([]int16, numSamples)

	phase := 0.0
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		progress := float64(i) / float64(numSamples)

		freq := startFreq + (endFreq-startFreq)*progress
		phase += 2.0 * math.Pi * freq / float64(sampleRate)

		attack := math.Min(1.0, t/0.006)
		decay := math.Exp(-t * 28.0)
		amp := volume * attack * decay

		val := amp * (0.85*math.Sin(phase) + 0.15*math.Sin(phase*2.0))
		samples[i] = int16(clamp(val * 32767))
	}

	return pcmToWav(samples, sampleRate)
}

// generateSuccessChime creates a soft, modern two-tone chime (G5 -> C6)
func generateSuccessChime() []byte {
	sampleRate := 44100
	t1 := 0.045
	t2 := 0.090
	totalSec := t1 + t2
	numSamples := int(float64(sampleRate) * totalSec)
	samples := make([]int16, numSamples)

	phase1 := 0.0
	phase2 := 0.0
	vol := 0.28

	freq1 := 783.99  // G5
	freq2 := 1046.50 // C6

	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		var val float64

		if t < t1 {
			phase1 += 2.0 * math.Pi * freq1 / float64(sampleRate)
			attack := math.Min(1.0, t/0.005)
			decay := math.Exp(-t * 22.0)
			val = vol * attack * decay * math.Sin(phase1)
		} else {
			tNote := t - t1
			phase2 += 2.0 * math.Pi * freq2 / float64(sampleRate)
			attack := math.Min(1.0, tNote/0.005)
			decay := math.Exp(-tNote * 24.0)
			val = vol * 1.1 * attack * decay * (0.88*math.Sin(phase2) + 0.12*math.Sin(phase2*2.0))
		}

		samples[i] = int16(clamp(val * 32767))
	}

	return pcmToWav(samples, sampleRate)
}

func clamp(v float64) float64 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return v
}

func pcmToWav(samples []int16, sampleRate int) []byte {
	var buf bytes.Buffer
	dataSize := uint32(len(samples) * 2)
	fileSize := 36 + dataSize

	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, fileSize)
	buf.WriteString("WAVE")

	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))

	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, dataSize)

	for _, s := range samples {
		_ = binary.Write(&buf, binary.LittleEndian, s)
	}

	return buf.Bytes()
}
