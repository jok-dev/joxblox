package ui

// The audio decode/metadata/playback engine moved to the Fyne-free
// joxblox/internal/audio package so the Wails backend can reuse it (audio
// duration on the web Single Asset view) without linking Fyne. These aliases
// keep the legacy Fyne UI compiling against the original unqualified names
// during the migration; new code should import internal/audio directly.

import "joxblox/internal/audio"

type (
	AssetAudioPlayer   = audio.AssetAudioPlayer
	AudioPlayerStatus  = audio.AudioPlayerStatus
	AudioMetadata      = audio.AudioMetadata
	DecodedAudioBuffer = audio.DecodedAudioBuffer
)

const DefaultAudioVolume = audio.DefaultAudioVolume

var (
	NewAssetAudioPlayer  = audio.NewAssetAudioPlayer
	DecodeAudioBuffer    = audio.DecodeAudioBuffer
	ExtractAudioMetadata = audio.ExtractAudioMetadata
	IsAudioContent       = audio.IsAudioContent
	FormatDurationCompact = audio.FormatDurationCompact
)
