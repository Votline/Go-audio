// Package audio init.go provides portaudio initialization.
package audio

import (
	"fmt"

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
