// Package compressor implements audio compression using Opus and Zstandard.
package compressor

import (
	"fmt"

	"github.com/hraban/opus"
	"github.com/klauspost/compress/zstd"
)

// Compressor handles audio compress and decompress.
// Used compression is Opus and Zstandard.
// It converts float32 PCM, from PortAudio, and int16 PCM, from Opus.
type Compressor struct {
	// bitrate is the target audio bitrate in bits per second (used by compressor).
	bitrate int

	// channels is the number of audio channels (e.g., 1 for mono, 2 for stereo).
	channels int

	// sampleRate is the audio sampling rate in Hz (e.g., 44100, 48000).
	sampleRate int

	// samplerPerFrame is the number of samples expected in one frame
	// (calculated based on duration and sample rate).
	samplerPerFrame int

	// duration is the duration of an audio frame in milliseconds.
	duration int

	// opusEn is the Opus encoder.
	opusEn *opus.Encoder

	// zstdEn is the Zstandard encoder.
	zstdEn *zstd.Encoder

	// encBuf is a pre-allocated buffer for storing encoded Opus packets
	// before they are compressed by Zstandard.
	encBuf []byte

	// opusDe is the Opus decoder.
	opusDe *opus.Decoder

	// zstdDe is the Zstandard decoder.
	zstdDe *zstd.Decoder

	// decBuf is a pre-allocated buffer for storing decoded int16 PCM samples
	// from Opus before conversion back to float32.
	decBuf []int16
}

// Init creates and initializes a new Compressor with the specified parameters.
// It sets up both Opus and Zstandard encoders/decoders and allocates internal buffers.
// Check Compressor struct for more details about arguments.
func Init(bitrate, channels, sampleRate, duration int, encBuf []byte, decBuf []int16) (*Compressor, error) {
	const op = "compressor.Init"

	opusEn, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, fmt.Errorf("%s: opus.NewEncoder: %w", op, err)
	}

	opusDe, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("%s: opus.NewDecoder: %w", op, err)
	}

	zstdEn, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, fmt.Errorf("%s: zstd.NewWriter: %w", op, err)
	}

	zstdDe, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("%s: zstd.NewReader: %w", op, err)
	}

	samplerPerFrame := (sampleRate * duration / 1000) * channels

	return &Compressor{
		bitrate:         bitrate,
		channels:        channels,
		sampleRate:      sampleRate,
		duration:        duration,
		samplerPerFrame: samplerPerFrame,

		opusEn: opusEn,
		zstdEn: zstdEn,
		encBuf: encBuf,

		opusDe: opusDe,
		zstdDe: zstdDe,
		decBuf: decBuf,
	}, nil
}

// Compress covners float32 PCM to int16, encodes it with Opus
// and compresses it with Zstandard.
func (c *Compressor) Compress(data []float32) ([]byte, error) {
	const op = "compressor.Compress"

	pcm := make([]int16, len(data))
	for i, v := range data {
		pcm[i] = int16(v * 32767)
	}

	enc, err := c.encode(pcm)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return c.zstdEn.EncodeAll(enc, nil), nil
}

// Decompress decodes int16 PCM from Opus and decompresses it with Zstandard.
// It converts float32 PCM back to PCM.
func (c *Compressor) Decompress(data []byte) ([]float32, error) {
	const op = "compressor.Decompress"

	if len(data) == 0 {
		return nil, nil
	}

	dec, err := c.zstdDe.DecodeAll(data, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: zstd.DecodeAll: %w", op, err)
	}

	pcmI16, err := c.decode(dec)
	if err != nil {
		return nil, fmt.Errorf("%s: opus.Decode: %w", op, err)
	}

	pcm := make([]float32, len(pcmI16))
	for i, v := range pcmI16 {
		pcm[i] = float32(v) / 32767
	}

	return pcm, nil
}

// encode encodes int16 PCM with Opus.
func (c *Compressor) encode(pcm []int16) ([]byte, error) {
	const op = "compressor.encode"

	n, err := c.opusEn.Encode(pcm, c.encBuf)
	if err != nil {
		return nil, fmt.Errorf("%s: opus.Encode: %w", op, err)
	}

	return c.encBuf[:n], nil
}

// decode decodes int16 PCM with Opus.
func (c *Compressor) decode(data []byte) ([]int16, error) {
	const op = "compressor.decode"

	n, err := c.opusDe.Decode(data, c.decBuf)
	if err != nil {
		return nil, fmt.Errorf("%s: opus.Decode: %w", op, err)
	}
	return c.decBuf[:n*c.channels], nil
}
