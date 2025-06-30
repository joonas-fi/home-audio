package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
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
				return wyomingTextToSpeech(cmd.Context(), serverAddr, args[0], os.Stdout)
			}
			return wyomingDescribe(serverAddr)

		},
	}
	cli.AddLogLevelControls(cmd.Flags())
	return cmd
}

func wyomingTextToSpeech(ctx context.Context, serverAddr string, phrase string, output io.Writer) error {
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

	// we're receiving a stream of audio whose length we're beforehand unsure of, hence we can't know the
	// # of samples (unless we do buffering), so just lie that we have an hour of audio. (I guess it's less wrong for
	// the audio data to be shorter than spec'd versus longer than spec's as that could leave the player to stop prematurely)
	audioLength := 1 * time.Hour
	numSamplesLie := int(audioLength.Seconds()) * audioHeader.Rate
	wavWriter := wav.NewWriter(
		output,
		uint32(numSamplesLie),
		uint16(audioHeader.Channels),
		uint32(audioHeader.Rate),
		uint16(audioHeader.Width*8))

	for {
		audioChunk, err := wyomingServer.ReadAnyResponse()
		switch { // expecting either an audio chunk or audio stop event.
		case err != nil:
			return err
		case audioChunk.msg.Type == wyomingCommandAudioStop: // job here is done
			return nil
		default:
			if err := audioChunk.msg.Type.ExpectToBe(wyomingCommandAudioChunk); err != nil {
				return err
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// continue
		}

		// got audio chunk

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
