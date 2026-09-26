package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	modWinmm                  = syscall.NewLazyDLL("winmm.dll")
	procWaveInOpen            = modWinmm.NewProc("waveInOpen")
	procWaveInClose           = modWinmm.NewProc("waveInClose")
	procWaveInPrepareHeader   = modWinmm.NewProc("waveInPrepareHeader")
	procWaveInUnprepareHeader = modWinmm.NewProc("waveInUnprepareHeader")
	procWaveInAddBuffer       = modWinmm.NewProc("waveInAddBuffer")
	procWaveInStart           = modWinmm.NewProc("waveInStart")
	procWaveInStop            = modWinmm.NewProc("waveInStop")
	procWaveInReset           = modWinmm.NewProc("waveInReset")
	procPlaySoundW            = modWinmm.NewProc("PlaySoundW")
)

const (
	WAVE_MAPPER       = 0xFFFFFFFF
	WAVE_FORMAT_PCM   = 1
	CALLBACK_FUNCTION = 0x00030000
	WIM_DATA          = 0x3C0
	SND_FILENAME      = 0x00020000
	SND_SYNC          = 0x00000000
	SND_ASYNC         = 0x00000001
)

type WAVEFORMATEX struct {
	WFormatTag      uint16
	NChannels       uint16
	NSamplesPerSec  uint32
	NAvgBytesPerSec uint32
	NBlockAlign     uint16
	WBitsPerSample  uint16
	CbSize          uint16
}

type WAVEHDR struct {
	LpData          uintptr
	DwBufferLength  uint32
	DwBytesRecorded uint32
	DwUser          uintptr
	DwFlags         uint32
	DwLoops         uint32
	LpNext          uintptr
	Reserved        uintptr
}

type Recorder struct {
	mu           sync.Mutex
	isRecording  bool
	hWaveIn      uintptr
	callbackPtr  uintptr
	numBuffers   int
	bufferSize   int
	buffers      [][]byte
	headers      []WAVEHDR
	preRoll      [][]byte
	phraseChunks [][]byte
	maxPreRoll   int
}

var globalRecorder *Recorder
var globalRecorderMu sync.Mutex

func waveInCallback(hwi uintptr, uMsg uint32, dwInstance uintptr, dwParam1 uintptr, dwParam2 uintptr) uintptr {
	if uMsg == WIM_DATA {
		hdr := (*WAVEHDR)(unsafe.Pointer(dwParam1))
		if hdr.DwBytesRecorded > 0 {
			buf := make([]byte, hdr.DwBytesRecorded)
			src := unsafe.Slice((*byte)(unsafe.Pointer(hdr.LpData)), hdr.DwBytesRecorded)
			copy(buf, src)

			globalRecorderMu.Lock()
			r := globalRecorder
			if r != nil {
				if r.isRecording {
					r.phraseChunks = append(r.phraseChunks, buf)
				} else {
					// Keep rolling buffer of recent audio
					r.preRoll = append(r.preRoll, buf)
					if len(r.preRoll) > r.maxPreRoll {
						r.preRoll = r.preRoll[len(r.preRoll)-r.maxPreRoll:]
					}
				}
			}
			globalRecorderMu.Unlock()

			// Re-add buffer to keep continuous stream running
			procWaveInAddBuffer.Call(hwi, dwParam1, unsafe.Sizeof(*hdr))
		}
	}
	return 0
}

func NewRecorder() *Recorder {
	cb := syscall.NewCallback(waveInCallback)
	return &Recorder{
		callbackPtr: cb,
		numBuffers:  12,
		bufferSize:  1600, // 50ms per buffer (16000 * 2 * 0.05)
		maxPreRoll:  7,    // ~350ms pre-roll lookback
	}
}

// InitDevice pre-warms the audio device once at startup so recording starts with 0ms latency
func (r *Recorder) InitDevice() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	sampleRate := uint32(16000)
	channels := uint16(1)
	bitsPerSample := uint16(16)
	blockAlign := channels * (bitsPerSample / 8)
	avgBytesPerSec := sampleRate * uint32(blockAlign)

	wfx := WAVEFORMATEX{
		WFormatTag:      WAVE_FORMAT_PCM,
		NChannels:       channels,
		NSamplesPerSec:  sampleRate,
		NAvgBytesPerSec: avgBytesPerSec,
		NBlockAlign:     blockAlign,
		WBitsPerSample:  bitsPerSample,
		CbSize:          0,
	}

	var hWaveIn uintptr
	ret, _, _ := procWaveInOpen.Call(
		uintptr(unsafe.Pointer(&hWaveIn)),
		uintptr(WAVE_MAPPER),
		uintptr(unsafe.Pointer(&wfx)),
		r.callbackPtr,
		0,
		CALLBACK_FUNCTION,
	)
	if ret != 0 {
		return fmt.Errorf("waveInOpen failed: %d", ret)
	}

	r.hWaveIn = hWaveIn
	r.buffers = make([][]byte, r.numBuffers)
	r.headers = make([]WAVEHDR, r.numBuffers)

	for i := 0; i < r.numBuffers; i++ {
		r.buffers[i] = make([]byte, r.bufferSize)
		r.headers[i] = WAVEHDR{
			LpData:         uintptr(unsafe.Pointer(&r.buffers[i][0])),
			DwBufferLength: uint32(r.bufferSize),
		}
		procWaveInPrepareHeader.Call(hWaveIn, uintptr(unsafe.Pointer(&r.headers[i])), unsafe.Sizeof(r.headers[i]))
		procWaveInAddBuffer.Call(hWaveIn, uintptr(unsafe.Pointer(&r.headers[i])), unsafe.Sizeof(r.headers[i]))
	}

	globalRecorderMu.Lock()
	globalRecorder = r
	globalRecorderMu.Unlock()

	// Start continuous background stream
	procWaveInStart.Call(hWaveIn)
	return nil
}

func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.hWaveIn == 0 {
		return
	}

	procWaveInStop.Call(r.hWaveIn)
	procWaveInReset.Call(r.hWaveIn)

	for i := 0; i < r.numBuffers; i++ {
		procWaveInUnprepareHeader.Call(r.hWaveIn, uintptr(unsafe.Pointer(&r.headers[i])), unsafe.Sizeof(r.headers[i]))
	}
	procWaveInClose.Call(r.hWaveIn)
	r.hWaveIn = 0

	globalRecorderMu.Lock()
	globalRecorder = nil
	globalRecorderMu.Unlock()
}

// Start captures the active phrase with 0ms startup delay, prepending the pre-roll lookback
func (r *Recorder) Start() {
	globalRecorderMu.Lock()
	defer globalRecorderMu.Unlock()

	r.phraseChunks = nil
	// Prepend the pre-roll buffers so the beginning of the word is NEVER clipped
	if len(r.preRoll) > 0 {
		r.phraseChunks = append(r.phraseChunks, r.preRoll...)
		r.preRoll = nil
	}
	r.isRecording = true
}

// Stop captures the remaining speech, normalizes audio volume, and returns clean PCM
func (r *Recorder) Stop() ([]byte, float64, float64) {
	// Small 80ms sleep to ensure the last trailing syllable is collected
	time.Sleep(80 * time.Millisecond)

	globalRecorderMu.Lock()
	r.isRecording = false
	chunks := r.phraseChunks
	r.phraseChunks = nil
	globalRecorderMu.Unlock()

	var rawPCM []byte
	for _, chunk := range chunks {
		rawPCM = append(rawPCM, chunk...)
	}

	if len(rawPCM) == 0 {
		return nil, 0, 1.0
	}

	normalized, peakPercent, scale := NormalizePCM(rawPCM)
	return normalized, peakPercent, scale
}

// NormalizePCM amplifies quiet microphone recordings to target ~75% peak volume
func NormalizePCM(pcm []byte) ([]byte, float64, float64) {
	if len(pcm) < 2 {
		return pcm, 0, 1.0
	}

	numSamples := len(pcm) / 2
	maxAmp := int16(0)

	for i := 0; i < numSamples; i++ {
		val := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		if val < 0 {
			if val == -32768 {
				val = 32767
			} else {
				val = -val
			}
		}
		if val > maxAmp {
			maxAmp = val
		}
	}

	peakPercent := (float64(maxAmp) / 32767.0) * 100.0
	if maxAmp == 0 {
		return pcm, peakPercent, 1.0
	}

	targetAmp := float64(24000)
	scale := targetAmp / float64(maxAmp)

	if scale > 12.0 {
		scale = 12.0
	}
	if scale < 1.0 {
		scale = 1.0
	}

	normalized := make([]byte, len(pcm))
	for i := 0; i < numSamples; i++ {
		val := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		scaled := float64(val) * scale
		if scaled > 32767 {
			scaled = 32767
		} else if scaled < -32768 {
			scaled = -32768
		}
		binary.LittleEndian.PutUint16(normalized[i*2:], uint16(int16(scaled)))
	}

	return normalized, peakPercent, scale
}

func PlayAudioFile(filepath string, sync bool) error {
	pathPtr, err := syscall.UTF16PtrFromString(filepath)
	if err != nil {
		return err
	}
	flags := SND_FILENAME
	if sync {
		flags |= SND_SYNC
	} else {
		flags |= SND_ASYNC
	}
	r, _, _ := procPlaySoundW.Call(uintptr(unsafe.Pointer(pathPtr)), 0, uintptr(flags))
	if r == 0 {
		return fmt.Errorf("PlaySound failed")
	}
	return nil
}

func EncodeWAV(pcm []byte, sampleRate uint32, channels uint16, bitsPerSample uint16) []byte {
	var buf bytes.Buffer

	byteRate := sampleRate * uint32(channels) * uint32(bitsPerSample/8)
	blockAlign := channels * (bitsPerSample / 8)
	dataSize := uint32(len(pcm))
	chunkSize := 36 + dataSize

	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, chunkSize)
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, channels)
	binary.Write(&buf, binary.LittleEndian, sampleRate)
	binary.Write(&buf, binary.LittleEndian, byteRate)
	binary.Write(&buf, binary.LittleEndian, blockAlign)
	binary.Write(&buf, binary.LittleEndian, bitsPerSample)
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, dataSize)
	buf.Write(pcm)

	return buf.Bytes()
}

func SaveWAVFile(filepath string, pcm []byte) error {
	wavBytes := EncodeWAV(pcm, 16000, 1, 16)
	return os.WriteFile(filepath, wavBytes, 0644)
}
