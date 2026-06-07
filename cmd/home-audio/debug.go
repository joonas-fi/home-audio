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

	collectedForResampling := []int{}

	audioChunksProcessed := 0

	for {
		audioChunk, err := wyomingServer.ReadAnyResponse()
		switch { // expecting either an audio chunk or audio stop event.
		case err != nil:
			return err
		case audioChunk.msg.Type == wyomingCommandAudioStop: // job here is done
			if shouldResample {
				resampled := downsamplePCM16WavValues(collectedForResampling, audioHeader.Rate, *sampleRate)
				samples := make([]wav.Sample, len(resampled))
				for i := range resampled {
					samples[i].Values[0] = resampled[i]
				}

				slog.Info("resampled write path", "audioChunksProcessed", audioChunksProcessed)

				if err := wavWriter.WriteSamples(samples); err != nil {
					return err
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

		if shouldResample {
			if audioHeader.Channels != 1 {
				panic("stereo resampling not supported")
			}

			for sampleIdx, samplesInChunk := 0, len(audioChunk.payload)/audioHeader.Width/audioHeader.Channels; sampleIdx < samplesInChunk; sampleIdx++ {
				for ch := 0; ch < audioHeader.Channels; ch++ {
					sampleOffset := sampleIdx * audioHeader.Width * audioHeader.Channels
					channelOffset := ch * audioHeader.Width
					offset := sampleOffset + channelOffset
					collectedForResampling = append(collectedForResampling, sampleReader(audioChunk.payload[offset:]))
				}
			}
		} else {
			samplesInChunk := len(audioChunk.payload) / audioHeader.Width / audioHeader.Channels
			samples := make([]wav.Sample, samplesInChunk)
			for sampleIdx := 0; sampleIdx < len(samples); sampleIdx++ {
				for ch := 0; ch < audioHeader.Channels; ch++ {
					sampleOffset := sampleIdx * audioHeader.Width * audioHeader.Channels
					channelOffset := ch * audioHeader.Width
					offset := sampleOffset + channelOffset
					samples[sampleIdx].Values[ch] = sampleReader(audioChunk.payload[offset:])
				}
			}

			if err := wavWriter.WriteSamples(samples); err != nil {
				return err
			}
		}

	}
}

func resolveSampleReader(bitsPerSample int) (func([]byte) int, error) {
	switch bitsPerSample {
	case 16:
		return func(buf []byte) int {
			return int(binary.LittleEndian.Uint16(buf))
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

func downsamplePCM16WavValues(pcmData []int, srcSampleRate, dstSampleRate int) []int {
	signedPcmData := make([]int, len(pcmData))
	for i, sample := range pcmData {
		signedPcmData[i] = int(int16(uint16(sample)))
	}

	signedDownsampled := downsamplePCM[int](signedPcmData, srcSampleRate, dstSampleRate)

	wavValues := make([]int, len(signedDownsampled))
	for i, sample := range signedDownsampled {
		wavValues[i] = int(uint16(int16(sample)))
	}

	return wavValues
}

// downsamplePCM downsamples the sample rate to the given value using averaging values.
// An example of reducing the sampling frequency from 48000 hertz to 16000 hertz:
// PcmDownsample[int16]([]int16{...}, 48000, 16000)
//
// Note: attempting to upsample will return the result unchanged
func downsamplePCM[U, T Number](pcmData []T, srcSampleRate, dstSampleRate int) []U {
	sampleRateRatio := srcSampleRate / dstSampleRate
	if sampleRateRatio <= 1 {
		return convertNumbers[U](pcmData)
	}

	newPcmData := make([]U, len(pcmData)/sampleRateRatio)
	var offsetResult = 0
	var offsetBuffer = 0
	for offsetResult < len(newPcmData) {
		var nextOffsetBuffer = int(math.Round(float64(offsetResult+1) * float64(sampleRateRatio)))
		// Use average value of skipped samples
		var accum float64
		var count float64
		for i := offsetBuffer; i < nextOffsetBuffer && i < len(pcmData); i++ {
			accum += float64(pcmData[i])
			count++
		}

		newPcmData[offsetResult] = U(accum / count)
		offsetResult++
		offsetBuffer = nextOffsetBuffer
	}

	return newPcmData
}

// convertNumbers casts a slice of numbers to a slice of numbers of another type.
// Example convert []int to []float32: convertNumbers[float32]([]int{1, 2, 3, 4, 5})
func convertNumbers[U, T Number](s []T) []U {
	out := make([]U, len(s))
	for i := range s {
		out[i] = U(s[i])
	}
	return out
}

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64
}
