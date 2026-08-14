//go:build !darwin

package nativesurface

import (
	"fmt"
	"unsafe"
)

type unsupportedBatchDriver struct{}

func NewNativeBackend() Backend {
	return newNativeBackend(unsupportedBatchDriver{})
}

func (unsupportedBatchDriver) apply(_ unsafe.Pointer, _ []nativeOperation) ([]nativeResult, error) {
	return nil, fmt.Errorf("native surface compositor is not implemented on this platform")
}
