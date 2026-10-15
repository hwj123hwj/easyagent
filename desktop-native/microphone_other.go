//go:build !darwin

package main

import "errors"

func startMicrophone() (voiceRecorder, error) {
	return nil, errors.New("此原生试用版的录音目前仅支持 macOS")
}
