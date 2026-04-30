// Package audio provides a simple audio client.
// Here is Record and Play raw PCM or compressed audio.
package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/gordonklaus/portaudio"
)

// Record starts recording from the default input device and writes compressed
// audio packets into w.
//
// Packet format is:
// - uint32 little-endian payload size
// - payload bytes (compressed audio chunk)
//
// Recording continues until StopRecording is called. If logFunc is provided via
// Init, it is used for diagnostics without introducing a logging dependency
// into the public API.
func (a *AudioClient) Record(w io.Writer) error {
	const op = "audio.Record"

	a.inpRBuf.Reset()
	a.inpQueue.Reset()
	a.isRecording = true

	log := a.logFunc

	samplePerMs := int(a.sampleRate*a.duration/1000) * a.channels

	stream, err := portaudio.OpenStream(
		portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   a.inpDevice,
				Channels: a.channels,
			},
			SampleRate:      float64(a.sampleRate),
			FramesPerBuffer: samplePerMs,
		},
		func(in []float32) {
			a.inpRBuf.Write(in)
			if log != nil {
				log(fmt.Sprintf("DEBUG op=%s msg=Recorded len=%d", op, len(in)))
			}
		})
	if err != nil {
		return fmt.Errorf("%s: create rec stream: %w", op, err)
	}

	if err := stream.Start(); err != nil {
		return fmt.Errorf("%s: start rec stream: %w", op, err)
	}

	defer stream.Stop()
	defer stream.Close()

	var wg sync.WaitGroup
	wg.Go(func() {
		for a.isRecording {
			streamBuf := make([]float32, a.readSize)
			a.inpRBuf.ReadAll(streamBuf, len(streamBuf))
			a.inpQueue.Push(streamBuf)
			if log != nil {
				log(fmt.Sprintf("DEBUG op=%s msg=Pushed to input queue len=%d", op, len(streamBuf)))
			}
		}
	})

	fileBuf := make([]float32, samplePerMs)
	chunk := make([]byte, samplePerMs)
	for a.isRecording {
		n := a.inpQueue.Pop(fileBuf)
		if n == 0 {
			time.Sleep(time.Millisecond)
			continue
		}

		if log != nil {
			log(fmt.Sprintf("DEBUG op=%s msg=Popped from input queue len=%d", op, n))
		}

		if err := a.ConvertRecord(fileBuf[:n], &chunk); err != nil {
			if log != nil {
				log(fmt.Sprintf("ERROR op=%s msg=Compression error err=%v", op, err))
			}
			continue
		}

		size := uint32(len(chunk))
		if err := binary.Write(w, binary.LittleEndian, size); err != nil {
			if log != nil {
				log(fmt.Sprintf("ERROR op=%s msg=Write error err=%v", op, err))
			}
			continue
		}

		if _, err := w.Write(chunk); err != nil {
			if log != nil {
				log(fmt.Sprintf("ERROR op=%s msg=Write error err=%v", op, err))
			}
			continue
		}
	}

	wg.Wait()

	return nil
}

// ConvertRecord converts raw PCM to compressed audio or []byte
func (a *AudioClient) ConvertRecord(pcm []float32, buf *[]byte) error {
	const op = "audio.ConvertRecord"

	if len(pcm) == 0 {
		*buf = []byte{}
		return nil
	}

	if a.useCompressor {
		temp, err := a.inpCmpr.Compress(pcm)
		if err != nil {
			return fmt.Errorf("%s: compression error: %w", op, err)
		}
		*buf = temp
		return nil
	}

	*buf = unsafe.Slice((*byte)(unsafe.Pointer(&pcm[0])), len(pcm)*4)
	return nil
}

// Play reads compressed audio packets from r (in the same format as produced
// by Record), decodes them and plays them to the default output device.
//
// Playback continues until EOF is reached or StopPlay is called. If logFunc
// is provided via Init, it is used for diagnostics.
func (a *AudioClient) Play(r io.Reader) error {
	const op = "audio.Play"

	a.outRBuf.Reset()
	a.isPlaying = true

	log := a.logFunc

	samplePerMs := int(a.sampleRate*a.duration/1000) * a.channels

	stream, err := portaudio.OpenStream(
		portaudio.StreamParameters{
			Output: portaudio.StreamDeviceParameters{
				Device:   a.outDevice,
				Channels: a.channels,
			},
			SampleRate:      float64(a.sampleRate),
			FramesPerBuffer: samplePerMs,
		},
		func(in, out []float32) {
			n := a.outRBuf.Read(out)
			for i := n; i < len(out); i++ {
				out[i] = 0
			}
			if log != nil {
				log(fmt.Sprintf("DEBUG op=%s msg=Playing len=%d", op, n))
			}
		})
	if err != nil {
		return fmt.Errorf("%s: create play stream: %w", op, err)
	}

	if err := stream.Start(); err != nil {
		return fmt.Errorf("%s: start play stream: %w", op, err)
	}

	defer stream.Stop()
	defer stream.Close()

	fromFile := make([]byte, samplePerMs)
	pcm := make([]float32, samplePerMs)
	var size uint32
	for a.isPlaying {
		err := binary.Read(r, binary.LittleEndian, &size)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			if log != nil {
				log(fmt.Sprintf("DEBUG op=%s msg=Playing finished", op))
			}
			break
		}
		if err != nil {
			if log != nil {
				log(fmt.Sprintf("ERROR op=%s msg=Read error err=%v", op, err))
			}
			continue
		}

		if log != nil {
			log(fmt.Sprintf("DEBUG op=%s msg=Read packet size=%d", op, int(size)))
		}

		if size > uint32(len(fromFile)) {
			fromFile = make([]byte, size)
		}
		buf := fromFile[:size]

		if _, err := io.ReadFull(r, buf); err != nil {
			if log != nil {
				log(fmt.Sprintf("ERROR op=%s msg=Read error err=%v", op, err))
			}
			continue
		}

		if err = a.ConvertPlay(buf, &pcm); err != nil {
			if log != nil {
				log(fmt.Sprintf("ERROR op=%s msg=Decompression error err=%v", op, err))
			}
			continue
		}

		a.outRBuf.Write(pcm)

		if log != nil {
			log(fmt.Sprintf("DEBUG op=%s msg=Write packet size=%d", op, len(pcm)))
		}
	}

	for a.outRBuf.Len() > 0 {
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(time.Duration(a.duration) * time.Millisecond)

	return nil
}

// ConvertPlay converts compressed audio or []byte to raw PCM
func (a *AudioClient) ConvertPlay(buf []byte, pcm *[]float32) error {
	const op = "audio.ConvertPlay"

	if len(buf) == 0 {
		*pcm = []float32{}
		return nil
	}

	if a.useCompressor {
		temp, err := a.outCmpr.Decompress(buf)
		if err != nil {
			return fmt.Errorf("%s: decompression error: %w", op, err)
		}
		*pcm = temp
		return nil
	}

	*pcm = unsafe.Slice((*float32)(unsafe.Pointer(&buf[0])), len(buf)/4)
	return nil
}

// StopPlay requests stopping the Replay loop.
func (acl *AudioClient) StopPlay() {
	acl.isPlaying = false
}

// IsPlaying reports whether Replay is currently running.
func (acl *AudioClient) IsPlaying() bool {
	return acl.isPlaying
}

// StopRecording requests stopping the Record loop.
func (acl *AudioClient) StopRecording() {
	acl.isRecording = false
}

// IsRecording reports whether Record is currently running.
func (acl *AudioClient) IsRecording() bool {
	return acl.isRecording
}
