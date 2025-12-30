package mail_test

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/emersion/go-smtp"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMail(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Mail")
}

var DefaultContext context.Context

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn(setup.WithoutDummyData)
	setupTestSMTPServer()
})

var _ = ginkgo.AfterSuite(func() {
	setup.AfterSuiteFn()
	if smtpServer != nil {
		if err := smtpServer.Close(); err != nil {
			logger.Errorf("Failed to close SMTP server: %v", err)
		}
	}
})

var (
	smtpServer   *smtp.Server
	smtpEndpoint string
	smtpPort     int
	testBackend  *captureBackend
)

type capturedMessage struct {
	From string
	To   []string
	Data []byte
}

type captureBackend struct {
	mu       sync.Mutex
	messages []capturedMessage
}

func (b *captureBackend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &captureSession{backend: b}, nil
}

func (b *captureBackend) GetMessages() []capturedMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]capturedMessage, len(b.messages))
	copy(result, b.messages)
	return result
}

func (b *captureBackend) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = nil
}

type captureSession struct {
	backend *captureBackend
	from    string
	to      []string
}

func (s *captureSession) Mail(from string, _ *smtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *captureSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}

func (s *captureSession) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	s.backend.mu.Lock()
	s.backend.messages = append(s.backend.messages, capturedMessage{
		From: s.from,
		To:   s.to,
		Data: data,
	})
	s.backend.mu.Unlock()
	return nil
}

func (s *captureSession) Reset() {
	s.from = ""
	s.to = nil
}

func (s *captureSession) Logout() error {
	return nil
}

func setupTestSMTPServer() {
	testBackend = &captureBackend{}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		ginkgo.Fail(err.Error())
	}

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		ginkgo.Fail("failed to parse port")
	}
	smtpPort = addr.Port
	smtpEndpoint = fmt.Sprintf("127.0.0.1:%d", smtpPort)

	smtpServer = smtp.NewServer(testBackend)
	smtpServer.Domain = "localhost"
	smtpServer.AllowInsecureAuth = true

	go func() {
		defer ginkgo.GinkgoRecover()
		logger.Infof("Starting test SMTP server on %s", smtpEndpoint)
		if err := smtpServer.Serve(listener); err != nil {
			logger.Infof("SMTP server stopped: %v", err)
		}
	}()
}
