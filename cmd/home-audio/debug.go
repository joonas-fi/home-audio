package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"time"

	"github.com/function61/gokit/app/cli"
	. "github.com/function61/gokit/builtin"
	"github.com/function61/gokit/encoding/jsonfile"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	resampling "github.com/tphakala/go-audio-resampler"
	"github.com/youpy/go-wav"
)

func debugEntrypoint() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug [phrase]",
		Short: "debug",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverAddr := "192.168.1.105:10200"
			if true {
				return wyomingTextToSpeech(cmd.Context(), serverAddr, args[0], os.Stdout, nil)
			}
			return wyomingDescribe(serverAddr)

		},
	}
	cli.AddLogLevelControls(cmd.Flags())
	return cmd
}

func wyomingTextToSpeech(ctx context.Context, serverAddr string, phrase string, output io.Writer, sampleRate *int) error {
	if err := ErrorIfUnset(phrase == "", "phrase"); err != nil {
		return err
	}

	wyomingServer, err := wyomingConnect(serverAddr)
	if err != nil {
		return err
	}

	if err := wyomingServer.Send(wyomingMsg{
		Type: wyomingCommandSynthesize,
		Data: wyomingData{
			Text: phrase,
			// NOTE: there doesn't seem to be sample rate support for synthesize command
			// https://github.com/rhasspy/rhasspy3/blob/master/docs/wyoming.md
		},
	}); err != nil {
		return err
	}

	audioStartResp, err := wyomingServer.ReadResponse(wyomingCommandAudioStart)
	if err != nil {
		return err
	}

	audioHeader := AudioStartData{}
	if err := jsonfile.UnmarshalAllowUnknownFields(bytes.NewReader(audioStartResp.data), &audioHeader); err != nil {
		return err
	}
	if err := audioHeader.Validate(); err != nil {
		return err
	}

	bitsPerSample := audioHeader.Width * 8

	slog.Info("audio start",
		"rate", audioHeader.Rate,
		"bits", bitsPerSample,
		"channels", audioHeader.Channels)

	sampleReader, err := resolveSampleReader(bitsPerSample)
	if err != nil {
		return err
	}

	outputSampleRate := uint32(audioHeader.Rate)
	shouldResample := sampleRate != nil
	if shouldResample {
		outputSampleRate = uint32(*sampleRate)
	}

	// we're receiving a stream of audio whose length we're beforehand unsure of, hence we can't know the
	// # of samples (unless we do buffering), so just lie that we have an hour of audio. (I guess it's less wrong for
	// the audio data to be shorter than spec'd versus longer than spec's as that could leave the player to stop prematurely)
	audioLength := 1 * time.Hour
	numSamplesLie := int(audioLength.Seconds()) * audioHeader.Rate
	wavWriter := wav.NewWriter(
		output,
		uint32(numSamplesLie),
		uint16(audioHeader.Channels),
		outputSampleRate,
		uint16(audioHeader.Width*8))

	outputSamples := func(samples [][]float64) error {
		asWavSamples := make([]wav.Sample, len(samples[0]))

		for idx := range asWavSamples {
			for ch := 0; ch < audioHeader.Channels; ch++ {
				asWavSamples[idx].Values[ch] = floatToPCM(samples[ch][idx])
			}
		}
		return wavWriter.WriteSamples(asWavSamples)
	}

	resampler, err := resampling.New(&resampling.Config{
		InputRate:  float64(audioHeader.Rate),
		OutputRate: float64(outputSampleRate),
		Channels:   audioHeader.Channels,
		Quality:    resampling.QualitySpec{Preset: resampling.QualityMedium},
	})
	if err != nil {
		return err
	}

	audioChunksProcessed := 0

	for {
		audioChunk, err := wyomingServer.ReadAnyResponse()
		switch { // expecting either an audio chunk or audio stop event.
		case err != nil:
			return err
		case audioChunk.msg.Type == wyomingCommandAudioStop: // job here is done
			slog.Info("resampled write path", "audioChunksProcessed", audioChunksProcessed)

			if shouldResample {
				// god knows what reason we've to have distinct paths for mono-vs-stereo flushing
				if multiFlusher, ok := resampler.(resampling.MultiFlusher); ok {
					remainingSamples, err := multiFlusher.FlushMulti()
					if err != nil {
						return err
					}
					if err := outputSamples(remainingSamples); err != nil {
						return err
					}
				} else {
					remainingSamples, err := resampler.Flush()
					if err != nil {
						return err
					}
					if err := outputSamples([][]float64{remainingSamples}); err != nil {
						return err
					}
				}
			}

			return nil
		default:
			if err := audioChunk.msg.Type.ExpectToBe(wyomingCommandAudioChunk); err != nil {
				return err
			}

			audioChunksProcessed++
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// continue
		}

		// got audio chunk
		numSamplesInChunk := len(audioChunk.payload) / audioHeader.Width / audioHeader.Channels

		// need to convert something like `int16` samples to float64 (for each channel)
		samplesForChannel := make([][]float64, audioHeader.Channels)
		for channel := range audioHeader.Channels {
			samplesForChannel[channel] = make([]float64, numSamplesInChunk)
		}
		for sampleIdx := range numSamplesInChunk {
			for channel := range audioHeader.Channels {
				sampleOffset := sampleIdx * audioHeader.Width * audioHeader.Channels
				channelOffset := channel * audioHeader.Width
				offset := sampleOffset + channelOffset
				samplesForChannel[channel][sampleIdx] = sampleReader(audioChunk.payload[offset:])
			}
		}

		if shouldResample {
			resampled, err := resampler.ProcessMulti(samplesForChannel)
			if err != nil {
				return err
			}

			if err := outputSamples(resampled); err != nil {
				return err
			}
		} else {
			if err := outputSamples(samplesForChannel); err != nil {
				return err
			}
		}
	}
}

func resolveSampleReader(bitsPerSample int) (func([]byte) float64, error) {
	switch bitsPerSample {
	case 16:
		return func(buf []byte) float64 {
			const half = math.MaxUint16 / 2
			return (float64(binary.LittleEndian.Uint16(buf)) - half) / (half)

		}, nil
	default:
		return nil, fmt.Errorf("%d bits per sample not supported", bitsPerSample)
	}
}

func wyomingDescribe(serverAddr string) error {
	wyomingServer, err := wyomingConnect(serverAddr)
	if err != nil {
		return err
	}

	resp, err := wyomingServer.ReadResponse(wyomingCommandInfo)
	if err != nil {
		return err
	}

	data := Data{}
	if err := jsonfile.UnmarshalAllowUnknownFields(bytes.NewReader(resp.data), &data); err != nil {
		return err
	}

	const preferredLanguage = "en_US"

	matchingVoices := lo.Filter(data.TTS[0].Voices, func(v Voice, _ int) bool { return v.Languages[0] == preferredLanguage })

	for _, match := range matchingVoices {
		fmt.Printf("%v\n", match)
	}

	return nil
}

// [-1...1] => 0..MaxUint16
func floatToPCM(input float64) int {
	return int((1 + input) * (math.MaxUint16 / 2))
}
