package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/inspektor-gadget/inspektor-gadget/pkg/datasource"
	igjson "github.com/inspektor-gadget/inspektor-gadget/pkg/datasource/formatters/json"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/factory"
	igapi "github.com/inspektor-gadget/inspektor-gadget/pkg/gadget-service/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const (
	gadgetServicePort      = uint16(igapi.GadgetServicePort)
	gadgetServiceDialLimit = 5 * time.Second
	gadgetStopWaitLimit    = 30 * time.Second
)

type realTraceRunner struct{}

type gadgetServiceTarget struct {
	podName string
	node    string
}

type k8sPortForwardConn struct {
	conn    httpstream.Connection
	stream  httpstream.Stream
	podName string
}

type k8sPodAddr struct {
	podName string
}

func NewTraceRunner() TraceRunner {
	return realTraceRunner{}
}

func (realTraceRunner) Run(ctx context.Context, req TraceRunRequest, emit func(TraceEvent)) error {
	if err := validateRunRequest(req); err != nil {
		return err
	}
	namespace := req.GadgetNamespace
	if namespace == "" {
		namespace = defaultSettings().GadgetNamespace
	}
	targets, err := gadgetServiceTargets(ctx, req.RESTConfig, namespace, requestedNodes(req.Params))
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("no gadget service pods found in namespace %q", namespace)
	}

	var errs []error
	for _, target := range targets {
		if err := runGadgetOnTarget(ctx, req, namespace, target, emit); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", target.node, err))
		}
	}
	return errors.Join(errs...)
}

func runGadgetOnTarget(ctx context.Context, req TraceRunRequest, namespace string, target gadgetServiceTarget, emit func(TraceEvent)) error {
	dialCtx, cancelDial := context.WithTimeout(ctx, gadgetServiceDialLimit)
	defer cancelDial()
	conn, err := dialGadgetService(dialCtx, req.RESTConfig, namespace, target, gadgetServicePort, gadgetServiceDialLimit)
	if err != nil {
		return fmt.Errorf("dial gadget service pod %s: %w", target.podName, err)
	}
	defer conn.Close()

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	client := igapi.NewGadgetManagerClient(conn)
	stream, err := client.RunGadget(streamCtx)
	if err != nil {
		return fmt.Errorf("open RunGadget stream: %w", err)
	}

	if err := stream.Send(&igapi.GadgetControlRequest{
		Event: &igapi.GadgetControlRequest_RunRequest{
			RunRequest: &igapi.GadgetRunRequest{
				ImageName:   req.Image,
				ParamValues: req.Params,
				Version:     igapi.VersionGadgetRunProtocol,
				Timeout:     int64(req.Timeout),
			},
		},
	}); err != nil {
		return fmt.Errorf("send run request: %w", err)
	}

	done := make(chan error, 1)
	go receiveGadgetEvents(stream, req, target, emit, done)

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = stream.Send(&igapi.GadgetControlRequest{Event: &igapi.GadgetControlRequest_StopRequest{StopRequest: &igapi.GadgetStopRequest{}}})
		select {
		case err := <-done:
			return err
		case <-time.After(gadgetStopWaitLimit):
			return fmt.Errorf("timed out waiting for gadget service stop confirmation")
		}
	}
}

func receiveGadgetEvents(stream igapi.GadgetManager_RunGadgetClient, req TraceRunRequest, target gadgetServiceTarget, emit func(TraceEvent), done chan<- error) {
	dsByID := map[uint32]datasource.DataSource{}
	initialized := false

	for {
		ev, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				done <- nil
			} else {
				done <- err
			}
			return
		}

		switch {
		case ev.Type == igapi.EventTypeGadgetInfo:
			if err := loadGadgetInfo(ev.Payload, dsByID, target, emit); err != nil {
				done <- err
				return
			}
			initialized = true
		case ev.Type == igapi.EventTypeGadgetPayload:
			if !initialized {
				emit(TraceEvent{Node: target.node, Error: "received gadget payload before gadget info"})
				continue
			}
			if err := emitGadgetPayload(ev, dsByID); err != nil {
				emit(TraceEvent{Node: target.node, Error: err.Error()})
			}
		case ev.Type == igapi.EventTypeGadgetResult:
			if len(ev.Payload) > 0 {
				event := decodeEvent(ev.Payload)
				if event.Node == "" {
					event.Node = target.node
				}
				emit(event)
			}
		case ev.Type == igapi.EventTypeGadgetDone:
			done <- nil
			return
		case ev.Type >= 1<<igapi.EventLogShift:
			emit(TraceEvent{Node: target.node, Raw: string(ev.Payload)})
		}
	}
}

func loadGadgetInfo(payload []byte, dsByID map[uint32]datasource.DataSource, target gadgetServiceTarget, emit func(TraceEvent)) error {
	info := &igapi.GadgetInfo{}
	if err := proto.Unmarshal(payload, info); err != nil {
		return fmt.Errorf("decode gadget info: %w", err)
	}
	for _, remoteDS := range info.DataSources {
		ds, err := datasource.NewFromAPI(remoteDS)
		if err != nil {
			return fmt.Errorf("create datasource %s: %w", remoteDS.Name, err)
		}
		formatter, err := igjson.New(ds, igjson.WithShowAll(true))
		if err != nil {
			return fmt.Errorf("create json formatter for datasource %s: %w", ds.Name(), err)
		}
		if err := ds.Subscribe(func(_ datasource.DataSource, data datasource.Data) error {
			raw := append([]byte(nil), formatter.Marshal(data)...)
			event := decodeEvent(raw)
			if event.Node == "" {
				event.Node = target.node
			}
			emit(event)
			return nil
		}, 50000); err != nil {
			return fmt.Errorf("subscribe datasource %s: %w", ds.Name(), err)
		}
		dsByID[remoteDS.Id] = ds
	}
	return nil
}

func emitGadgetPayload(ev *igapi.GadgetEvent, dsByID map[uint32]datasource.DataSource) error {
	ds := dsByID[ev.DataSourceID]
	if ds == nil {
		return fmt.Errorf("datasource id %d not found", ev.DataSourceID)
	}
	var packet datasource.Packet
	var err error
	switch ds.Type() {
	case datasource.TypeSingle:
		packet, err = ds.NewPacketSingleFromRaw(ev.Payload)
	case datasource.TypeArray:
		packet, err = ds.NewPacketArrayFromRaw(ev.Payload)
	default:
		return fmt.Errorf("unsupported datasource type %d", ds.Type())
	}
	if err != nil {
		return err
	}
	packet.SetSeq(ev.Seq)
	return ds.EmitAndRelease(packet)
}

func gadgetServiceTargets(ctx context.Context, config *rest.Config, namespace string, nodes []string) ([]gadgetServiceTarget, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "k8s-app=gadget"})
	if err != nil {
		return nil, fmt.Errorf("list gadget pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no gadget pods found in namespace %q", namespace)
	}
	if len(nodes) == 0 {
		targets := make([]gadgetServiceTarget, 0, len(pods.Items))
		for _, pod := range pods.Items {
			targets = append(targets, gadgetServiceTarget{podName: pod.Name, node: pod.Spec.NodeName})
		}
		return targets, nil
	}

	targets := make([]gadgetServiceTarget, 0, len(nodes))
	for _, node := range nodes {
		found := false
		for _, pod := range pods.Items {
			if pod.Spec.NodeName == node {
				targets = append(targets, gadgetServiceTarget{podName: pod.Name, node: node})
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("node %q does not have a gadget pod", node)
		}
	}
	return targets, nil
}

func requestedNodes(params map[string]string) []string {
	node := strings.TrimSpace(params["node"])
	if node == "" {
		return nil
	}
	parts := strings.Split(node, ",")
	nodes := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			nodes = append(nodes, part)
		}
	}
	return nodes
}

func dialGadgetService(ctx context.Context, config *rest.Config, namespace string, target gadgetServiceTarget, port uint16, timeout time.Duration) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithReturnConnectionError(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return newK8SPortForwardConn(ctx, config, namespace, target.podName, port, timeout)
		}),
	}
	return grpc.DialContext(ctx, "passthrough:///"+target.podName, opts...)
}

func newK8SPortForwardConn(_ context.Context, config *rest.Config, namespace, podName string, targetPort uint16, timeout time.Duration) (net.Conn, error) {
	conn := &k8sPortForwardConn{podName: podName}
	cfg := rest.CopyConfig(config)
	factory.SetKubernetesDefaults(cfg)
	cfg.Timeout = timeout

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes SPDY roundtripper: %w", err)
	}
	targetURL, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("parse rest config host: %w", err)
	}
	targetURL.Path = fmt.Sprintf("api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, targetURL)
	newConn, _, err := dialer.Dial(portforward.PortForwardProtocolV1Name)
	if err != nil {
		return nil, err
	}

	headers := http.Header{}
	headers.Set(corev1.StreamType, corev1.StreamTypeError)
	headers.Set(corev1.PortHeader, fmt.Sprintf("%d", targetPort))
	headers.Set(corev1.PortForwardRequestIDHeader, strconv.Itoa(1))
	errorStream, err := newConn.CreateStream(headers)
	if err != nil {
		newConn.Close()
		return nil, fmt.Errorf("create port-forward error stream: %w", err)
	}
	errorStream.Close()
	go func() {
		if message, err := io.ReadAll(errorStream); err == nil && len(message) > 0 {
			_ = newConn.Close()
		}
	}()

	headers.Set(corev1.StreamType, corev1.StreamTypeData)
	dataStream, err := newConn.CreateStream(headers)
	if err != nil {
		newConn.Close()
		return nil, fmt.Errorf("create port-forward data stream: %w", err)
	}
	conn.conn = newConn
	conn.stream = dataStream
	return conn, nil
}

func (k *k8sPortForwardConn) Read(b []byte) (int, error) {
	return k.stream.Read(b)
}

func (k *k8sPortForwardConn) Write(b []byte) (int, error) {
	return k.stream.Write(b)
}

func (k *k8sPortForwardConn) Close() error {
	k.stream.Close()
	return k.conn.Close()
}

func (k *k8sPortForwardConn) LocalAddr() net.Addr {
	return &k8sPodAddr{podName: k.podName}
}

func (k *k8sPortForwardConn) RemoteAddr() net.Addr {
	return &k8sPodAddr{podName: k.podName}
}

func (k *k8sPortForwardConn) SetDeadline(time.Time) error {
	return nil
}

func (k *k8sPortForwardConn) SetReadDeadline(time.Time) error {
	return nil
}

func (k *k8sPortForwardConn) SetWriteDeadline(time.Time) error {
	return nil
}

func (a *k8sPodAddr) Network() string {
	return "k8s-pod"
}

func (a *k8sPodAddr) String() string {
	return a.podName
}
