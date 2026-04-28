// Package goaudio is entry point for Go-audio library.
// Contains all needed functions.
package goaudio

import (
	"github.com/Votline/Go-audio/pkg/audio"
	"github.com/Votline/Go-audio/pkg/compressor"
	"github.com/Votline/Go-audio/pkg/queue"
	rb "github.com/Votline/Go-audio/pkg/ringbuffer"
)

// InitAudioClient initializes the audio client.
// Used Init function from audio package.
func InitAudioClient(bufSize, queueSize, readSize, cmprSize, channels, bitrate, sampleRate, duration int, useCompressor bool, logFunc func(string)) (*audio.AudioClient, error) {
	return audio.Init(bufSize, queueSize, readSize, cmprSize, channels, bitrate, sampleRate, duration, useCompressor, logFunc)
}

// InitCompressor initializes the compressor.
// Used Init function from compressor package.
func InitCompressor(bitrate, channels, sampleRate, duration int, encBuf []byte, decBuf []int16) (*compressor.Compressor, error) {
	return compressor.Init(bitrate, channels, sampleRate, duration, encBuf, decBuf)
}

// NewRingBuffer creates a new RingBuffer.
// Used NewRB function from ringbuffer package.
func NewRingBuffer(bufSize uint64) *rb.RingBuffer {
	return rb.NewRB(bufSize)
}

func NewQueue(bufLen int) *queue.Queue {
	return queue.New(bufLen)
}
