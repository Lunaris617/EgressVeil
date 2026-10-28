//go:build !linux

package main

import "errors"

func execute(op string) (map[string]any, error) {
	return map[string]any{"operation": op}, errors.New("Linux required; no changes performed")
}
