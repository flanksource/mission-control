package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/google/gops/signal"
)

type GopsClient struct {
	Addr    string
	Timeout time.Duration
}

func (c GopsClient) Version(ctx context.Context) (string, error) {
	out, err := c.command(ctx, signal.Version)
	return string(out), err
}

func (c GopsClient) Stats(ctx context.Context) (string, error) {
	out, err := c.command(ctx, signal.Stats)
	return string(out), err
}

func (c GopsClient) MemStats(ctx context.Context) (string, error) {
	out, err := c.command(ctx, signal.MemStats)
	return string(out), err
}

func (c GopsClient) Stack(ctx context.Context) (string, error) {
	out, err := c.command(ctx, signal.StackTrace)
	return string(out), err
}

func (c GopsClient) Heap(ctx context.Context) ([]byte, error) {
	return c.command(ctx, signal.HeapProfile)
}

func (c GopsClient) CPU(ctx context.Context) ([]byte, error) {
	return c.command(ctx, signal.CPUProfile)
}

func (c GopsClient) Trace(ctx context.Context) ([]byte, error) {
	return c.command(ctx, signal.Trace)
}

func (c GopsClient) command(ctx context.Context, sig byte, params ...byte) ([]byte, error) {
	if c.Addr == "" {
		return nil, fmt.Errorf("gops address is required")
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return nil, fmt.Errorf("connect gops %s: %w", c.Addr, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	buf := []byte{sig}
	buf = append(buf, params...)
	if _, err := conn.Write(buf); err != nil {
		return nil, fmt.Errorf("send gops command: %w", err)
	}
	out, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("read gops response: %w", err)
	}
	return out, nil
}

func gopsAddr(sess *Session) string {
	return fmt.Sprintf("127.0.0.1:%d", sess.GopsLocal)
}
