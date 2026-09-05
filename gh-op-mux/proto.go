package main

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	typRequest  uint8 = 1
	typStdin    uint8 = 2
	typStdinEOF uint8 = 3
	typStdout   uint8 = 4
	typStderr   uint8 = 5
	typExit     uint8 = 6
	typError    uint8 = 7
	typSignal   uint8 = 8
)

const maxFrame = 1 << 20

var errFrameTooLarge = errors.New("gh-op-mux: frame too large")

func writeFrame(w io.Writer, typ uint8, payload []byte) error {
	var hdr [5]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(1+len(payload)))
	hdr[4] = typ
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

func readFrame(r io.Reader) (uint8, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[0:4])
	if n < 1 || n > maxFrame {
		return 0, nil, errFrameTooLarge
	}
	typ := hdr[4]
	payloadLen := int(n - 1)
	if payloadLen == 0 {
		return typ, nil, nil
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return typ, payload, nil
}
