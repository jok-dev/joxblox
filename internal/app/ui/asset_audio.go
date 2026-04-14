package ui

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"joxblox/internal/format"
	"joxblox/internal/roblox"

	"github.com/faiface/beep"
	"github.com/faiface/beep/effects"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/vorbis"
	"github.com/faiface/beep/wav"
)

type AudioMetadata struct {
	Duration time.Duration
	Format   string
}

const DefaultAudioVolume = 0.4

type AudioPlayerStatus struct {
	Available bool
	Playing   bool
	Paused    bool
	Message   string
	Position  time.Duration
	Duration  time.Duration
	Volume    float64
}

type AssetAudioPlayer struct {
	mutex              sync.Mutex
	buffer             *beep.Buffer
	duration           time.Duration
	streamer           beep.StreamSeeker
	volumeEffect       *effects.Volume
	positionSamples    int
	volumeValue        float64
	ctrl               *beep.Ctrl
	playbackToken      uint64
	speakerInitialized bool
	speakerSampleRate  beep.SampleRate
	onStatusChanged    func(AudioPlayerStatus)
}

type audioDecoderCandidate struct {
	key    string
	format string
	open   func([]byte) (beep.StreamSeekCloser, beep.Format, error)
}

func NewAssetAudioPlayer(onStatusChanged func(AudioPlayerStatus)) *AssetAudioPlayer {
	return &AssetAudioPlayer{
		onStatusChanged: onStatusChanged,
		volumeValue:     DefaultAudioVolume,
	}
}

func (player *AssetAudioPlayer) Load(fileName string, contentType string, fileBytes []byte) error {
	player.Reset()
	if len(fileBytes) == 0 {
		return fmt.Errorf("no audio bytes are available")
	}

	decodedAudio, err := DecodeAudioBuffer(fileName, contentType, fileBytes)
	if err != nil {
		player.emitStatus(AudioPlayerStatus{
			Available: false,
			Message:   fmt.Sprintf("Playback unavailable: %s", err.Error()),
		})
		return err
	}

	return player.LoadDecoded(decodedAudio)
}

func (player *AssetAudioPlayer) LoadDecoded(decodedAudio *DecodedAudioBuffer) error {
	if decodedAudio == nil || decodedAudio.Buffer == nil {
		return fmt.Errorf("no decoded audio is available")
	}

	player.mutex.Lock()
	initErr := player.ensureSpeakerLocked(decodedAudio.Format.SampleRate)
	if initErr != nil {
		player.mutex.Unlock()
		player.emitStatus(AudioPlayerStatus{
			Available: false,
			Message:   fmt.Sprintf("Playback unavailable: %s", initErr.Error()),
		})
		return initErr
	}
	player.buffer = decodedAudio.Buffer
	player.duration = decodedAudio.Metadata.Duration
	player.positionSamples = 0
	player.volumeValue = DefaultAudioVolume
	snapshot := player.snapshotLocked("Ready")
	player.mutex.Unlock()

	player.emitStatus(snapshot)
	return nil
}

func (player *AssetAudioPlayer) TogglePlayPause() error {
	player.mutex.Lock()
	if player.buffer == nil {
		player.mutex.Unlock()
		return fmt.Errorf("audio playback is not available")
	}

	if player.ctrl == nil {
		streamer := player.buffer.Streamer(0, player.buffer.Len())
		startPosition := format.Clamp(player.positionSamples, 0, player.buffer.Len())
		if startPosition >= player.buffer.Len() {
			startPosition = 0
		}
		if err := streamer.Seek(startPosition); err != nil {
			player.mutex.Unlock()
			return err
		}
		volumeEffect := &effects.Volume{Streamer: streamer, Base: 2}
		applyVolumeLevel(volumeEffect, player.volumeValue)
		ctrl := &beep.Ctrl{Streamer: volumeEffect}
		player.playbackToken++
		token := player.playbackToken
		player.streamer = streamer
		player.volumeEffect = volumeEffect
		player.ctrl = ctrl
		snapshot := player.snapshotLocked("Playing")
		player.mutex.Unlock()

		player.emitStatus(snapshot)
		speaker.Play(beep.Seq(ctrl, beep.Callback(func() {
			player.handlePlaybackFinished(token)
		})))
		go player.monitorPlayback(token)
		return nil
	}

	speaker.Lock()
	player.ctrl.Paused = !player.ctrl.Paused
	if player.streamer != nil {
		player.positionSamples = format.Clamp(player.streamer.Position(), 0, player.buffer.Len())
	}
	speaker.Unlock()
	message := "Playing"
	if player.ctrl.Paused {
		message = "Paused"
	}
	snapshot := player.snapshotLocked(message)
	player.mutex.Unlock()

	player.emitStatus(snapshot)
	return nil
}

func (player *AssetAudioPlayer) Stop() {
	player.mutex.Lock()
	hasAudio := player.buffer != nil
	player.playbackToken++
	player.ctrl = nil
	player.streamer = nil
	player.volumeEffect = nil
	player.positionSamples = 0
	snapshot := player.snapshotLocked("Ready")
	player.mutex.Unlock()

	if hasAudio {
		speaker.Clear()
		player.emitStatus(snapshot)
	}
}

func (player *AssetAudioPlayer) Reset() {
	player.mutex.Lock()
	hasAudio := player.buffer != nil
	player.playbackToken++
	player.ctrl = nil
	player.streamer = nil
	player.volumeEffect = nil
	player.buffer = nil
	player.duration = 0
	player.positionSamples = 0
	player.volumeValue = DefaultAudioVolume
	player.mutex.Unlock()

	if hasAudio {
		speaker.Clear()
	}
	player.emitStatus(AudioPlayerStatus{})
}

func (player *AssetAudioPlayer) SetVolume(volume float64) error {
	player.mutex.Lock()
	if player.buffer == nil {
		player.mutex.Unlock()
		return fmt.Errorf("audio playback is not available")
	}
	player.volumeValue = format.Clamp(volume, 0.0, 1.0)
	if player.volumeEffect != nil {
		speaker.Lock()
		applyVolumeLevel(player.volumeEffect, player.volumeValue)
		speaker.Unlock()
	}
	snapshot := player.snapshotLocked("")
	player.mutex.Unlock()

	player.emitStatus(snapshot)
	return nil
}

func (player *AssetAudioPlayer) SeekToFraction(fraction float64) error {
	player.mutex.Lock()
	if player.buffer == nil {
		player.mutex.Unlock()
		return fmt.Errorf("audio playback is not available")
	}
	targetPosition := format.Clamp(
		int(math.Round(format.Clamp(fraction, 0.0, 1.0)*float64(player.buffer.Len()))),
		0,
		player.buffer.Len(),
	)
	if player.streamer != nil {
		speaker.Lock()
		seekErr := player.streamer.Seek(targetPosition)
		if seekErr == nil {
			player.positionSamples = targetPosition
		}
		speaker.Unlock()
		if seekErr != nil {
			player.mutex.Unlock()
			return seekErr
		}
	} else {
		player.positionSamples = targetPosition
	}
	message := "Ready"
	if player.ctrl != nil {
		if player.ctrl.Paused {
			message = "Paused"
		} else {
			message = "Playing"
		}
	}
	snapshot := player.snapshotLocked(message)
	player.mutex.Unlock()

	player.emitStatus(snapshot)
	return nil
}

func (player *AssetAudioPlayer) handlePlaybackFinished(token uint64) {
	player.mutex.Lock()
	if token != player.playbackToken {
		player.mutex.Unlock()
		return
	}
	player.positionSamples = player.buffer.Len()
	player.ctrl = nil
	player.streamer = nil
	player.volumeEffect = nil
	hasAudio := player.buffer != nil
	snapshot := player.snapshotLocked("Finished")
	player.mutex.Unlock()

	if !hasAudio {
		return
	}
	player.emitStatus(snapshot)
}

func (player *AssetAudioPlayer) ensureSpeakerLocked(sampleRate beep.SampleRate) error {
	if sampleRate <= 0 {
		return fmt.Errorf("unsupported audio sample rate")
	}
	if player.speakerInitialized && player.speakerSampleRate == sampleRate {
		return nil
	}
	if player.speakerInitialized {
		speaker.Clear()
		speaker.Close()
		player.speakerInitialized = false
	}
	if err := speaker.Init(sampleRate, sampleRate.N(time.Second/10)); err != nil {
		return err
	}
	player.speakerInitialized = true
	player.speakerSampleRate = sampleRate
	return nil
}

func (player *AssetAudioPlayer) emitStatus(status AudioPlayerStatus) {
	if player.onStatusChanged != nil {
		player.onStatusChanged(status)
	}
}

func (player *AssetAudioPlayer) monitorPlayback(token uint64) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		player.mutex.Lock()
		if token != player.playbackToken || player.ctrl == nil || player.buffer == nil {
			player.mutex.Unlock()
			return
		}
		snapshot := player.snapshotLocked("")
		player.mutex.Unlock()
		player.emitStatus(snapshot)
	}
}

func (player *AssetAudioPlayer) snapshotLocked(message string) AudioPlayerStatus {
	status := AudioPlayerStatus{
		Available: player.buffer != nil,
		Duration:  player.duration,
		Volume:    player.volumeValue,
		Message:   strings.TrimSpace(message),
	}
	if player.buffer == nil {
		return status
	}
	positionSamples := format.Clamp(player.positionSamples, 0, player.buffer.Len())
	if player.streamer != nil {
		speaker.Lock()
		positionSamples = format.Clamp(player.streamer.Position(), 0, player.buffer.Len())
		speaker.Unlock()
		player.positionSamples = positionSamples
	}
	status.Position = samplesToDuration(player.buffer.Format().SampleRate, positionSamples)
	if status.Duration <= 0 {
		status.Duration = samplesToDuration(player.buffer.Format().SampleRate, player.buffer.Len())
	}
	if player.ctrl != nil {
		status.Playing = !player.ctrl.Paused
		status.Paused = player.ctrl.Paused
	}
	if status.Message == "" {
		if player.ctrl == nil {
			status.Message = "Ready"
		} else if player.ctrl.Paused {
			status.Message = "Paused"
		} else {
			status.Message = "Playing"
		}
	}
	return status
}

type DecodedAudioBuffer struct {
	Buffer   *beep.Buffer
	Format   beep.Format
	Metadata AudioMetadata
}

func ExtractAudioMetadata(fileName string, contentType string, fileBytes []byte) (*AudioMetadata, error) {
	streamer, format, formatName, err := openAudioDecoder(fileName, contentType, fileBytes)
	if err != nil {
		return nil, err
	}
	defer streamer.Close()

	duration := time.Duration(0)
	if streamer.Len() > 0 {
		duration = format.SampleRate.D(streamer.Len())
	}
	return &AudioMetadata{
		Duration: duration,
		Format:   formatName,
	}, nil
}

func DecodeAudioBuffer(fileName string, contentType string, fileBytes []byte) (*DecodedAudioBuffer, error) {
	streamer, format, formatName, err := openAudioDecoder(fileName, contentType, fileBytes)
	if err != nil {
		return nil, err
	}
	defer streamer.Close()

	audioBuffer := beep.NewBuffer(format)
	audioBuffer.Append(streamer)
	duration := time.Duration(0)
	if audioBuffer.Len() > 0 {
		duration = format.SampleRate.D(audioBuffer.Len())
	}
	return &DecodedAudioBuffer{
		Buffer: audioBuffer,
		Format: format,
		Metadata: AudioMetadata{
			Duration: duration,
			Format:   formatName,
		},
	}, nil
}

func samplesToDuration(sampleRate beep.SampleRate, sampleCount int) time.Duration {
	if sampleRate <= 0 || sampleCount <= 0 {
		return 0
	}
	return sampleRate.D(sampleCount)
}

func openAudioDecoder(fileName string, contentType string, fileBytes []byte) (beep.StreamSeekCloser, beep.Format, string, error) {
	candidates := orderedAudioDecoders(fileName, contentType)
	triedKeys := map[string]bool{}
	var lastErr error
	for _, candidate := range candidates {
		if candidate.key == "" || triedKeys[candidate.key] {
			continue
		}
		triedKeys[candidate.key] = true
		streamer, format, err := candidate.open(fileBytes)
		if err == nil {
			return streamer, format, candidate.format, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("unsupported audio format")
	}
	return nil, beep.Format{}, "", lastErr
}

func orderedAudioDecoders(fileName string, contentType string) []audioDecoderCandidate {
	trimmedContentType := strings.ToLower(strings.TrimSpace(contentType))
	extension := strings.ToLower(strings.TrimSpace(filepath.Ext(fileName)))

	allCandidates := map[string]audioDecoderCandidate{
		"ogg": {
			key:    "ogg",
			format: "OGG Vorbis",
			open: func(fileBytes []byte) (beep.StreamSeekCloser, beep.Format, error) {
				return vorbis.Decode(io.NopCloser(bytes.NewReader(fileBytes)))
			},
		},
		"mp3": {
			key:    "mp3",
			format: "MP3",
			open: func(fileBytes []byte) (beep.StreamSeekCloser, beep.Format, error) {
				return mp3.Decode(io.NopCloser(bytes.NewReader(fileBytes)))
			},
		},
		"wav": {
			key:    "wav",
			format: "WAV",
			open: func(fileBytes []byte) (beep.StreamSeekCloser, beep.Format, error) {
				return wav.Decode(bytes.NewReader(fileBytes))
			},
		},
	}

	orderedKeys := []string{}
	appendKey := func(key string) {
		if key == "" {
			return
		}
		orderedKeys = append(orderedKeys, key)
	}

	switch {
	case strings.Contains(trimmedContentType, "ogg"), strings.Contains(trimmedContentType, "vorbis"), extension == ".ogg":
		appendKey("ogg")
	case strings.Contains(trimmedContentType, "mpeg"), strings.Contains(trimmedContentType, "mp3"), extension == ".mp3":
		appendKey("mp3")
	case strings.Contains(trimmedContentType, "wav"), strings.Contains(trimmedContentType, "wave"), extension == ".wav":
		appendKey("wav")
	}

	appendKey("ogg")
	appendKey("mp3")
	appendKey("wav")

	candidates := make([]audioDecoderCandidate, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		candidate, exists := allCandidates[key]
		if !exists {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func IsAudioContent(assetTypeID int, contentType string) bool {
	if assetTypeID == roblox.AssetTypeAudio {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "audio/")
}

func FormatDurationCompact(duration time.Duration) string {
	if duration <= 0 {
		return "-"
	}
	totalSeconds := int(duration.Round(time.Second) / time.Second)
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func applyVolumeLevel(effect *effects.Volume, volume float64) {
	if effect == nil {
		return
	}
	clampedVolume := format.Clamp(volume, 0.0, 1.0)
	if clampedVolume <= 0 {
		effect.Silent = true
		effect.Volume = 0
		return
	}
	effect.Silent = false
	effect.Volume = math.Log2(clampedVolume)
}
