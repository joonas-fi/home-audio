package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"

	"github.com/function61/gokit/encoding/jsonfile"
)

// https://github.com/rhasspy/rhasspy3/blob/master/docs/wyoming.md

type client struct {
	writer io.Writer
	reader *bufio.Reader
}

func wyomingConnect(serverAddr string) (*client, error) {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		return nil, err
	}

	// cannot use scanner because it expects us to have same delimiter (like \n) to read line at a time,
	// but Wyoming protocol is mostly line-based except for the data chunks so we must support these read primitives:
	// 1) read until \ņ
	// 2) read N bytes
	reader := bufio.NewReader(conn)

	return &client{writer: conn, reader: reader}, nil
}

func (c *client) Send(msg wyomingMsg) error {
	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	if _, err := c.writer.Write([]byte(string(msgJSON) + "\n")); err != nil {
		return err
	}

	return nil
}

// complete Wyoming event with its data and payload read.
type wyomingEvent struct {
	msg     *wyomingMsg // parsed rigid JSON structure ("event header") for all events
	data    []byte      // event-specific JSON data
	payload []byte      // event-specific binary payload (such as audio chunk)
}

func (c *client) ReadAnyResponse() (*wyomingEvent, error) {
	withErr := func(err error) (*wyomingEvent, error) { return nil, fmt.Errorf("ReadResponse: %w", err) }

	responseLine, err := c.reader.ReadBytes('\n')
	if err != nil {
		return withErr(err)
	}

	slog.Debug("got", "responseLine", string(responseLine))

	resp := &wyomingMsg{}
	if err := jsonfile.UnmarshalAllowUnknownFields(bytes.NewReader(responseLine), resp); err != nil {
		return withErr(err)
	}

	dataBytes, err := func() ([]byte, error) {
		if resp.DataLength > 0 {
			rdr, err := c.readData(resp)
			if err != nil {
				return nil, err
			}
			return io.ReadAll(rdr)
		} else {
			return nil, nil
		}
	}()
	if err != nil {
		return nil, err
	}

	payloadBytes, err := func() ([]byte, error) {
		if resp.PayloadLength > 0 {
			rdr, err := c.readPayload(resp)
			if err != nil {
				return nil, err
			}
			return io.ReadAll(rdr)
		} else {
			return nil, nil
		}
	}()
	if err != nil {
		return nil, err
	}

	return &wyomingEvent{resp, dataBytes, payloadBytes}, nil
}

func (c *client) ReadResponse(expectedResponseType wyomingCommand) (*wyomingEvent, error) {
	e, err := c.ReadAnyResponse()
	if err != nil {
		return nil, err
	}
	if err := e.msg.Type.ExpectToBe(expectedResponseType); err != nil {
		return nil, fmt.Errorf("ReadResponse: %w", err)
	}

	return e, nil
}

func (c *client) readPayload(resp *wyomingMsg) (io.Reader, error) {
	if resp.PayloadLength == 0 {
		return nil, fmt.Errorf("response %s expected to have payload data but doesn't have", resp.Type)
	}

	return io.LimitReader(c.reader, int64(resp.PayloadLength)), nil
}

func (c *client) readData(resp *wyomingMsg) (io.Reader, error) {
	if resp.DataLength == 0 {
		return nil, fmt.Errorf("response %s expected to have data but doesn't have", resp.Type)
	}

	return io.LimitReader(c.reader, int64(resp.DataLength)), nil
}

type wyomingMsg struct {
	Type          wyomingCommand `json:"type"`
	Data          wyomingData    `json:"data,omitempty"`
	DataLength    int            `json:"data_length,omitempty"`
	PayloadLength int            `json:"payload_length,omitempty"`
}

type wyomingData struct {
	Text string `json:"text,omitempty"`
}

type wyomingCommand string

func (w wyomingCommand) ExpectToBe(expected wyomingCommand) error {
	if w != expected {
		return fmt.Errorf("wyomingCommand: expected %s; got %s", expected, w)
	}

	return nil
}

const (
	wyomingCommandDescribe   = "describe"
	wyomingCommandInfo       = "info"
	wyomingCommandSynthesize = "synthesize"
	wyomingCommandAudioStart = "audio-start"
	wyomingCommandAudioChunk = "audio-chunk"
	wyomingCommandAudioStop  = "audio-stop"
)

// Attribution represents the attribution details
type Attribution struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Voice represents an individual voice in the TTS system
type Voice struct {
	Name        string      `json:"name"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description"`
	Languages   []string    `json:"languages"`
	Speakers    interface{} `json:"speakers"` // Could be null or another type (e.g., a list or map)
}

// TTS represents a text-to-speech engine
type TTS struct {
	Name        string      `json:"name"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description"`
	Voices      []Voice     `json:"voices"`
}

// Data represents the full JSON structure
type Data struct {
	ASR []interface{} `json:"asr"` // Assuming ASR is an empty list for now
	TTS []TTS         `json:"tts"`
}

type AudioStartData struct {
	Rate     int `json:"rate"`
	Width    int `json:"width"`
	Channels int `json:"channels"`
}

func (a AudioStartData) Validate() error {
	if a.Rate == 0 || a.Width == 0 || a.Channels == 0 {
		return errors.New("invalid AudioStartData")
	}
	return nil
}
