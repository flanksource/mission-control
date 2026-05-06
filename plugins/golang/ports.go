package main

import (
	"fmt"
	"net"
	"strconv"
)

func pickLocalPort(preferred int) (int, error) {
	if preferred > 0 {
		return preferred, nil
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate local port: %w", err)
	}
	addr := l.Addr().String()
	if cerr := l.Close(); cerr != nil {
		return 0, fmt.Errorf("release probe listener: %w", cerr)
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}
