//go:build !darwin

package main

import "errors"

type systemSecrets struct{}

func (systemSecrets) Get(string) (string, error) {
	return "", errors.New("此原生试用版的安全凭据保存目前仅支持 macOS")
}
func (systemSecrets) Set(string, string) error {
	return errors.New("此原生试用版的安全凭据保存目前仅支持 macOS")
}
func (systemSecrets) Delete(string) error {
	return errors.New("此原生试用版的安全凭据保存目前仅支持 macOS")
}
