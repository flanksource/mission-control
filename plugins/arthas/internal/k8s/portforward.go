package k8s

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortMapping maps a local port to a remote container port.
type PortMapping struct {
	LocalPort  int
	RemotePort int
}

// Forwarder owns a running port-forward session.
type Forwarder struct {
	stop chan struct{}
	done chan error
}

// Ready blocks until the forwarder is accepting connections on all local ports
// or ctx is cancelled.
func (f *Forwarder) Ready(ctx context.Context, ready <-chan struct{}) error {
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops the forwarder. Returns the final error from the port-forward
// goroutine (usually nil).
func (f *Forwarder) Close() error {
	select {
	case <-f.stop:
		// already closed
	default:
		close(f.stop)
	}
	return <-f.done
}

// StartPortForward opens a port-forward to the given pod. Returns the forwarder
// and a channel that closes when forwarding is ready. The caller must call
// Close() to stop.
func StartPortForward(restCfg *rest.Config, namespace, pod string, ports []PortMapping, errOut, infoOut io.Writer) (*Forwarder, <-chan struct{}, error) {
	if len(ports) == 0 {
		return nil, nil, fmt.Errorf("no ports to forward")
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("build kube client: %w", err)
	}

	req := cs.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(namespace).
		Name(pod).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(restCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("spdy transport: %w", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", mustURL(req.URL().String()))

	specs := make([]string, 0, len(ports))
	for _, p := range ports {
		specs = append(specs, strconv.Itoa(p.LocalPort)+":"+strconv.Itoa(p.RemotePort))
	}

	stop := make(chan struct{})
	ready := make(chan struct{})

	pf, err := portforward.New(dialer, specs, stop, ready, infoOut, errOut)
	if err != nil {
		return nil, nil, fmt.Errorf("create port-forward: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- pf.ForwardPorts()
	}()

	return &Forwarder{stop: stop, done: done}, ready, nil
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(fmt.Errorf("portforward: invalid URL %q: %w", raw, err))
	}
	return u
}
