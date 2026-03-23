package app

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

func (view *assetView) configureAudioPlayback(statsInfo *imageInfo, assetTypeID int) {
	if view.audioPlayer == nil {
		return
	}
	loadToken := view.audioLoadToken.Add(1)
	view.audioPlayer.Reset()
	view.resetAudioControls()
	view.audioControls.Hide()
	if statsInfo == nil || !isAudioAssetContent(assetTypeID, statsInfo.ContentType) {
		return
	}
	view.audioControls.Show()
	if len(view.assetDownloadBytes) == 0 {
		view.updateAudioPlaybackState(audioPlayerStatus{
			Available: false,
			Message:   "No audio bytes are available for playback.",
		})
		return
	}
	view.updateAudioPlaybackState(audioPlayerStatus{
		Available: false,
		Duration:  statsInfo.Duration,
		Volume:    defaultAudioVolume,
		Message:   "Loading audio...",
	})
	fileName := view.assetDownloadFileName
	contentType := statsInfo.ContentType
	audioBytes := append([]byte(nil), view.assetDownloadBytes...)
	go func(expectedLoadToken uint64, currentAssetID int64) {
		decodedAudio, decodeErr := decodeAudioBuffer(fileName, contentType, audioBytes)
		fyne.Do(func() {
			if view.audioLoadToken.Load() != expectedLoadToken || view.currentAssetID != currentAssetID {
				return
			}
			if decodeErr != nil {
				view.updateAudioPlaybackState(audioPlayerStatus{
					Available: false,
					Duration:  statsInfo.Duration,
					Volume:    defaultAudioVolume,
					Message:   fmt.Sprintf("Playback unavailable: %s", decodeErr.Error()),
				})
				return
			}
			if loadErr := view.audioPlayer.LoadDecoded(decodedAudio); loadErr != nil {
				view.updateAudioPlaybackState(audioPlayerStatus{
					Available: false,
					Duration:  statsInfo.Duration,
					Volume:    defaultAudioVolume,
					Message:   fmt.Sprintf("Playback unavailable: %s", loadErr.Error()),
				})
			}
		})
	}(loadToken, view.currentAssetID)
}

func (view *assetView) updateAudioPlaybackState(status audioPlayerStatus) {
	apply := func() {
		if view.playAudioButton == nil || view.stopAudioButton == nil || view.audioProgressSlider == nil || view.audioVolumeSlider == nil {
			return
		}
		if status.Playing && !status.Paused {
			view.playAudioButton.Text = "Pause"
			view.playAudioButton.Icon = theme.MediaPauseIcon()
		} else {
			view.playAudioButton.Text = "Play"
			view.playAudioButton.Icon = theme.MediaPlayIcon()
		}
		if status.Available {
			view.playAudioButton.Enable()
			view.audioProgressSlider.Enable()
			view.audioVolumeSlider.Enable()
			if status.Playing || status.Paused || status.Position > 0 {
				view.stopAudioButton.Enable()
			} else {
				view.stopAudioButton.Disable()
			}
		} else {
			view.playAudioButton.Disable()
			view.stopAudioButton.Disable()
			view.audioProgressSlider.Disable()
			view.audioVolumeSlider.Disable()
		}
		view.audioDuration = status.Duration
		if !view.audioSeekDragging {
			view.audioCurrentTimeLabel.SetText(formatDurationCompact(status.Position))
		}
		view.audioTotalTimeLabel.SetText(formatDurationCompact(status.Duration))
		if !view.audioSeekDragging {
			view.suppressAudioSeekChange = true
			if status.Duration > 0 {
				view.audioProgressSlider.SetValue(clampAudioSliderValue(float64(status.Position) / float64(status.Duration)))
			} else {
				view.audioProgressSlider.SetValue(0)
			}
			view.suppressAudioSeekChange = false
		}
		view.suppressAudioVolumeChange = true
		view.audioVolumeSlider.SetValue(clampAudioSliderValue(status.Volume))
		view.suppressAudioVolumeChange = false
		view.audioVolumeValueLabel.SetText(fmt.Sprintf("%d%%", int(clampAudioSliderValue(status.Volume)*100)))
		view.playAudioButton.Refresh()
		view.stopAudioButton.Refresh()
		if view.audioControls != nil {
			view.audioControls.Refresh()
		}
	}
	if fyne.CurrentApp() == nil {
		apply()
		return
	}
	fyne.Do(apply)
}

func (view *assetView) resetAudioControls() {
	if view.audioCurrentTimeLabel != nil {
		view.audioCurrentTimeLabel.SetText("0:00")
	}
	if view.audioTotalTimeLabel != nil {
		view.audioTotalTimeLabel.SetText("0:00")
	}
	view.audioDuration = 0
	if view.audioProgressSlider != nil {
		view.suppressAudioSeekChange = true
		view.audioSeekDragging = false
		view.audioProgressSlider.SetValue(0)
		view.suppressAudioSeekChange = false
		view.audioProgressSlider.Disable()
	}
	if view.audioVolumeSlider != nil {
		view.suppressAudioVolumeChange = true
		view.audioVolumeSlider.SetValue(defaultAudioVolume)
		view.suppressAudioVolumeChange = false
		view.audioVolumeSlider.Disable()
	}
	if view.audioVolumeValueLabel != nil {
		view.audioVolumeValueLabel.SetText("40%")
	}
}
