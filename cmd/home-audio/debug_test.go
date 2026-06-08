package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/function61/gokit/testing/assert"
)

func TestFoo(t *testing.T) {
	mkBuf := func(input uint16) []byte {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, input)
		return b
	}
	reader, err := resolveSampleReader(16)
	assert.Ok(t, err)

	assert.Equal(t, fmt.Sprintf("%.02f", reader(mkBuf(0))), "-1.00")
	assert.Equal(t, fmt.Sprintf("%.02f", reader(mkBuf(math.MaxUint16/2))), "0.00")
	assert.Equal(t, fmt.Sprintf("%.02f", reader(mkBuf(math.MaxUint16))), "1.00")

	assert.Equal(t, floatToPCM(-1.0), 0)
	assert.Equal(t, floatToPCM(0.0), 32767)
	assert.Equal(t, floatToPCM(1.0), 65534)
}
