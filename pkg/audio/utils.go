package audio

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unsafe"

	"github.com/gordonklaus/portaudio"
)

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

// RemoveMonitor removes the monitor by sink name.
func (acl *AudioClient) RemoveMonitor(sinkName string) error {
	const op = "audio.RemoveMonitor"

	out, err := exec.Command("pactl", "list", "modules", "short").Output()
	if err != nil {
		return fmt.Errorf("%s: failed to list modules: %w", op, err)
	}

	lines := strings.Split(string(out), "\n")
	var lastErr error

	for _, line := range lines {
		if strings.Contains(line, sinkName) {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				moduleID := fields[0]
				if err := exec.Command("pactl", "unload-module", moduleID).Run(); err != nil {
					lastErr = err
				}
			}
		}
	}

	if lastErr != nil {
		return fmt.Errorf("%s: failed to unload some modules: %w", op, lastErr)
	}
	return nil
}

// MoveStreams moves streams to the target sink.
func MoveStreams(target, targetAppName string, isRecording bool) error {
	const op = "main.MoveStreams"

	listCmd := "sink-inputs"
	moveCmd := "move-sink-input"
	objectType := "Sink Input"

	if isRecording {
		listCmd = "source-outputs"
		moveCmd = "move-source-output"
		objectType = "Source Output"
		if !strings.HasSuffix(target, ".monitor") {
			target += ".monitor"
		}
	}

	out, err := exec.Command("pactl", "list", listCmd).Output()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	searchTag := []byte("application.name = \"" + targetAppName + "\"")
	blocks := bytes.Split(out, []byte("\n\n"))

	found := false
	for _, block := range blocks {
		if !bytes.Contains(block, searchTag) {
			continue
		}

		headerTag := []byte(objectType + " #")
		lines := bytes.Split(block, []byte("\n"))
		if len(lines) > 0 && bytes.HasPrefix(lines[0], headerTag) {
			parts := bytes.Split(lines[0], []byte("#"))
			if len(parts) > 1 {
				streamID := string(bytes.TrimSpace(parts[1]))
				exec.Command("pactl", moveCmd, streamID, target).Run()
				found = true
			}
		}
	}

	if !found {
		return fmt.Errorf("%s: app %s not found", op, targetAppName)
	}
	return nil
}

// MoveAllSinkInputs moves all sink inputs to the sink.
func MoveAllSinkInputs(sinkName string) error {
	const op = "main.moveAllSinkInputs"

	out, err := exec.Command("pactl", "list", "short", "sink-inputs").Output()
	if err != nil {
		return fmt.Errorf("%s: pactl list short sink-inputs: %w", op, err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		id := fields[0]
		exec.Command("pactl", "move-sink-input", id, sinkName).Run()
	}

	return nil
}

// SetDefaultSink sets for all new apps the default sink.
func SetDefaultSink(sinkName string) error {
	if err := exec.Command("pactl", "set-default-sink", sinkName).Run(); err != nil {
		return fmt.Errorf("set-default-sink: %w", err)
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
