//go:build !darwin || !mas

package models

func nativeStoreConfigDir() string { return "" }
