// Package audio init.go provides portaudio initialization.
package audio

import (
	"bytes"
	"fmt"
	"os/exec"
	"time"
	"unsafe"

	"github.com/Votline/Go-audio/pkg/compressor"
	"github.com/Votline/Go-audio/pkg/queue"
	rb "github.com/Votline/Go-audio/pkg/ringbuffer"

	"github.com/gordonklaus/portaudio"
)

const (
	isolateInputCode  = -1
	isolateOutputCode = -2
)

// AudioClient struct contains fields for working with audio.
type AudioClient struct {
	// bitrate is the target audio bitrate in bits per second (used by compressor).
	bitrate int

	// channels is the number of audio channels (e.g., 1 for mono, 2 for stereo).
	channels int

	// sampleRate is the audio sampling rate in Hz (e.g., 44100, 48000).
	sampleRate int

	// readSize is the number of samples to read from the input ring buffer
	// and push to the queue in each iteration of the recording loop.
	readSize int

	// duration is the buffer duration in milliseconds.
	// It determines the FramesPerBuffer size for PortAudio streams.
	duration int

	// inpRBuf is a ring buffer that temporarily stores raw float32 samples
	// from the PortAudio input callback before they are moved to inpQueue.
	inpRBuf *rb.RingBuffer

	// inpQueue is a queue of []float32 chunks holding recorded audio data
	// ready for compression and writing to the output stream.
	inpQueue *queue.Queue

	// inpCmpr is the compressor used to encode raw PCM data from the input
	// into a compressed format (e.g., Opus) before writing.
	inpCmpr *compressor.Compressor

	// inpDevice holds information about the selected PortAudio input device (microphone).
	inpDevice *portaudio.DeviceInfo

	// isRecording is a flag indicating whether the recording loop is active.
	isRecording bool

	// outRBuf is a ring buffer that stores decoded PCM samples ready for playback.
	// The PortAudio output callback reads from this buffer.
	outRBuf *rb.RingBuffer

	// outCmpr is the decompressor used to decode compressed audio data
	// from the input stream back into raw PCM for playback.
	outCmpr *compressor.Compressor

	// outDevice holds information about the selected PortAudio output device (speakers/headphones).
	outDevice *portaudio.DeviceInfo

	// isPlaying is a flag indicating whether the playback loop is active.
	isPlaying bool

	// useCompressor is a flag indicating whether to use the compressor.
	useCompressor bool

	// logFunc is a function for logging.
	// It is used instead of the default logger.
	logFunc func(string)
}

// Init initializes the audio client.
// If not arguments specified, default values are used.
// Check AudiClient struct for more details about arguments.
// If useCompressor is true, the audio client will init and use the compressor.
func Init(bufSize, queueSize, readSize, cmprSize, channels, bitrate, sampleRate, duration int, useCompressor bool, logFunc func(string)) (*AudioClient, error) {
	const op = "audio.Init"

	if channels <= 0 {
		channels = 2
	}
	if bufSize <= 0 {
		bufSize = 1920 * 4
	}
	if queueSize <= 0 {
		queueSize = 1920 * 4
	}
	if readSize <= 0 {
		readSize = 1920 * channels
	}
	if cmprSize <= 0 {
		cmprSize = 1920
	}

	if bitrate <= 0 {
		bitrate = 48000
	}
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	if duration <= 0 {
		duration = 20
	}

	if err := portaudio.Initialize(); err != nil {
		return nil, fmt.Errorf("%s: portaudio init: %w", op, err)
	}

	acl := &AudioClient{
		bitrate:    bitrate,
		channels:   channels,
		sampleRate: sampleRate,

		readSize: readSize,
		duration: duration,

		inpRBuf:   rb.NewRB(uint64(bufSize)),
		inpQueue:  queue.New(queueSize),
		inpDevice: nil,
		inpCmpr:   nil,

		outRBuf:   rb.NewRB(uint64(bufSize)),
		outDevice: nil,
		outCmpr:   nil,

		useCompressor: useCompressor,

		logFunc: logFunc,
	}

	if acl.initDevices(); acl.inpDevice == nil || acl.outDevice == nil {
		return nil, fmt.Errorf("%s: input or output device init failed", op)
	}

	if useCompressor {
		encInpBuf := make([]byte, cmprSize)
		encOutBuf := make([]byte, cmprSize)

		decInpBuf := make([]int16, cmprSize)
		decOutBuf := make([]int16, cmprSize)

		inpCmr, err := compressor.Init(acl.bitrate, acl.channels, acl.sampleRate, acl.duration, encInpBuf, decInpBuf)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}

		outCmr, err := compressor.Init(acl.bitrate, acl.channels, acl.sampleRate, acl.duration, encOutBuf, decOutBuf)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}

		acl.inpCmpr = inpCmr
		acl.outCmpr = outCmr
	}

	return acl, nil
}

// initDevices initializes the input and output devices.
// It tries to initialize the input device 10 times.
// Used default input and output devices.
func (a *AudioClient) initDevices() {
	const op = "audio.initDevices"

	var err error
	maxAttempts := 10
	for range maxAttempts {
		if a.inpDevice == nil {
			a.inpDevice, err = portaudio.DefaultInputDevice()
			if err != nil {
				a.inpDevice = nil
				continue
			}
		}
		if a.outDevice == nil {
			a.outDevice, err = portaudio.DefaultOutputDevice()
			if err != nil {
				a.outDevice = nil
				continue
			}
		}
	}
}

// AutoRouteMonitor redirects audio input source to the monitor.
// of the default output device.
// This typically used on Linux with PulseAudio/PipeWire.
func (acl *AudioClient) AutoRouteMonitor() error {
	const op = "audio.autoRouteMonitor"

	out, _ := exec.Command("pactl", "get-default-sink").Output()
	trimSpaceBytes(&out)

	monitorNameBytes := append(out, []byte(".monitor")...)
	monitorName := unsafe.String(unsafe.SliceData(monitorNameBytes), len(monitorNameBytes))

	time.Sleep(200 * time.Millisecond)

	out, err := exec.Command("pactl", "list", "source-outputs").Output()
	if err != nil {
		return err
	}

	rangeByByte(out, '\n', func(start, end int) {
		line := out[start:end]
		spaceIdx := bytes.IndexByte(line, ' ')
		if spaceIdx == -1 {
			return
		}
		streamIDBytes := line[:spaceIdx]
		streamID := unsafe.String(unsafe.SliceData(streamIDBytes), len(streamIDBytes))

		exec.Command("pactl", "move-source-output", streamID, monitorName).Run()
	})

	return nil
}

// SetInputByName sets the input device by name.
func (acl *AudioClient) SetInputByName(name string) error {
	const op = "audio.SetInputByName"

	if err := acl.setDeviceByName(name, func(device *portaudio.DeviceInfo) {
		acl.inpDevice = device
	}); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// SetOutputByName sets the output device by name.
func (acl *AudioClient) SetOutputByName(name string) error {
	const op = "audio.SetOutputByName"
	if err := acl.setDeviceByName(name, func(device *portaudio.DeviceInfo) {
		acl.outDevice = device
	}); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// setDeviceByName sets the device by name.
// It iterates over all devices and finds the one with the given name.
// If found, it yields the device info.
func (acl *AudioClient) setDeviceByName(name string, yield func(device *portaudio.DeviceInfo)) error {
	const op = "audio.setDeviceByName"

	devices, _ := portaudio.Devices()
	for _, device := range devices {
		if device.Name == name {
			yield(device)
			return nil
		}
	}
	return fmt.Errorf("device %s not found", name)
}

// IsolateInput isolates the input stream.
// Used isolateStream func
func (acl *AudioClient) IsolateInput(sinkName, appName string) error {
	const op = "audio.IsolateInput"
	if err := acl.isolateStream(sinkName, appName, isolateInputCode); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// IsolateOutput isolates the output stream.
// Used isolateStream func
func (acl *AudioClient) IsolateOutput(sinkName, appName string) error {
	const op = "audio.IsolateOutput"
	if err := acl.isolateStream(sinkName, appName, isolateOutputCode); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// isolateStream creates a new null sink and moves the stream to it.
// It is used to isolate the input stream from the rest of the system.
func (acl *AudioClient) isolateStream(sinkName, appName string, dev int) error {
	const op = "audio.isolateStream"

	var listCmd, moveCmd, objectType string
	switch dev {
	case isolateInputCode:
		listCmd, moveCmd, objectType = "source-outputs", "move-source-output", "Source Output"
	case isolateOutputCode:
		listCmd, moveCmd, objectType = "sink-inputs", "move-sink-input", "Sink Input"
	default:
		return fmt.Errorf("%s: invalid device code %d", op, dev)
	}

	exec.Command("pactl", "load-module", "module-null-sink",
		"sink_name="+sinkName,
		"sink_properties=device.description="+sinkName).Run()

	time.Sleep(100 * time.Millisecond) // wait for the module to load

	out, err := exec.Command("pactl", "list", listCmd).Output()
	if err != nil {
		return fmt.Errorf("%s: no sinks found: %w", op, err)
	}

	streamID := findStreamID(out, appName, objectType)
	if streamID == "" {
		return fmt.Errorf("%s: sink %s not found", op, sinkName)
	}

	exec.Command("pactl", moveCmd, streamID, sinkName).Run()

	exec.Command("pactl", "load-module", "module-loopback",
		"source="+sinkName+".monitor",
		"sink=@DEFAULT_SINK@").Run()

	return nil
}

// findStreamID finds the stream ID of the given app name.
func findStreamID(out []byte, appName, objectType string) string {
	blocks := bytes.Split(out, []byte("\n\n"))

	searchTag := []byte("application.name = \"" + appName + "\"")
	headerTag := []byte(objectType + " #")

	for _, block := range blocks {
		if bytes.Contains(block, searchTag) {
			lines := bytes.Split(block, []byte("\n"))
			if len(lines) <= 0 {
				continue
			}
			if bytes.HasPrefix(lines[0], headerTag) {
				parts := bytes.Split(lines[0], []byte("#"))
				if len(parts) > 1 {
					trimSpaceBytes(&parts[1])
					return string(parts[1])
				}
			}
		}
	}

	return ""
}

// trimSpaceBytes trims spaces in slice by pointer
func trimSpaceBytes(b *[]byte) {
	tempB := *b

	start := 0
	end := len(tempB) - 1
	for start < end && isSpace(tempB[start]) {
		start++
	}
	for end > start && isSpace(tempB[end]) {
		end--
	}

	*b = tempB[start : end+1]
}

// rangeByByte iterates over a slice by separator byte.
func rangeByByte(b []byte, sep byte, yield func(start, end int)) {
	start, end, sepIdx := 0, len(b)-1, 0

	for start < end {
		sepIdx = bytes.IndexByte(b[start:], sep)
		if sepIdx == -1 {
			break
		}
		start += sepIdx + 1 // jump to separator and skip it

		sepIdx = bytes.IndexByte(b[start:], sep)
		if sepIdx == -1 {
			break
		}
		end = start + sepIdx

		if start == end {
			break
		}

		yield(start, end)
	}
}

// isSpace is a helper function to check if a byte is a space.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
