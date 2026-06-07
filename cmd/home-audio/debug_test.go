package main

import (
	"reflect"
	"testing"
)

func TestDownsamplePCM16WavValuesAveragesInSignedSpace(t *testing.T) {
	got := downsamplePCM16WavValues([]int{0xffff, 0x0001, 0xffff, 0x0001}, 4, 2)
	want := []int{0x0000, 0x0000}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
