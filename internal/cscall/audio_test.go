package cscall

import (
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func TestAudioBridgeBuildRTPPacketSerializesSequenceState(t *testing.T) {
	const packetCount = 4096
	ab := &AudioBridge{ssrc: 0x12345678}

	type header struct {
		sequence  uint16
		timestamp uint32
	}
	headers := make(chan header, packetCount)
	var wg sync.WaitGroup
	for i := 0; i < packetCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			packet := ab.buildRTPPacket([]byte{0xff})
			headers <- header{
				sequence:  binary.BigEndian.Uint16(packet[2:4]),
				timestamp: binary.BigEndian.Uint32(packet[4:8]),
			}
		}()
	}
	wg.Wait()
	close(headers)

	seen := make(map[header]struct{}, packetCount)
	for got := range headers {
		if got.timestamp != uint32(got.sequence)*160 {
			t.Fatalf("RTP header sequence/timestamp pair = %d/%d", got.sequence, got.timestamp)
		}
		if _, duplicate := seen[got]; duplicate {
			t.Fatalf("duplicate RTP sequence state: %+v", got)
		}
		seen[got] = struct{}{}
	}
	if len(seen) != packetCount {
		t.Fatalf("unique RTP headers = %d, want %d", len(seen), packetCount)
	}
	if ab.seqNum != packetCount || ab.timestamp != packetCount*160 {
		t.Fatalf("final RTP state = %d/%d, want %d/%d", ab.seqNum, ab.timestamp, packetCount, packetCount*160)
	}
}

func TestAudioBridgeStartRejectsDuplicateAndStopReapsProcesses(t *testing.T) {
	ab, err := NewAudioBridge("hw:test", "test-audio-lifecycle")
	if err != nil {
		t.Fatalf("NewAudioBridge() error = %v", err)
	}
	ab.command = audioHelperCommand
	defer ab.Stop()

	if err := ab.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	captureCmd, playbackCmd := ab.captureCmd, ab.playbackCmd
	if err := ab.Start(); err == nil {
		t.Fatal("second Start() unexpectedly succeeded")
	}
	ab.Stop()
	if captureCmd.ProcessState == nil {
		t.Fatal("arecord process was not reaped")
	}
	if playbackCmd.ProcessState == nil {
		t.Fatal("aplay process was not reaped")
	}
}

func TestAudioBridgePlaybackStartFailureReapsCapture(t *testing.T) {
	ab, err := NewAudioBridge("hw:test", "test-audio-partial-start")
	if err != nil {
		t.Fatalf("NewAudioBridge() error = %v", err)
	}
	ab.command = func(name string, arg ...string) *exec.Cmd {
		if name == "aplay" {
			return exec.Command("/vodoge-test-command-does-not-exist")
		}
		return audioHelperCommand(name, arg...)
	}
	defer ab.Stop()

	if err := ab.Start(); err == nil {
		t.Fatal("Start() unexpectedly succeeded")
	}
	if ab.captureCmd == nil || ab.captureCmd.ProcessState == nil {
		t.Fatal("arecord process was not reaped after aplay start failure")
	}
}

func TestAudioBridgeHelperProcess(t *testing.T) {
	if os.Getenv("VODOGE_AUDIO_HELPER") != "1" {
		return
	}
	role := os.Args[len(os.Args)-1]
	switch role {
	case "arecord":
		frame := make([]byte, 640)
		for {
			if _, err := os.Stdout.Write(frame); err != nil {
				os.Exit(0)
			}
			time.Sleep(5 * time.Millisecond)
		}
	case "aplay":
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func audioHelperCommand(name string, _ ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAudioBridgeHelperProcess$", "--", name)
	cmd.Env = append(os.Environ(), "VODOGE_AUDIO_HELPER=1")
	return cmd
}
