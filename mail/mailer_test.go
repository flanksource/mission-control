package mail_test

import (
	"bytes"
	"io"
	"strings"

	gomsg "github.com/emersion/go-message"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/mail"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Mail", func() {
	ginkgo.BeforeEach(func() {
		testBackend.Clear()
	})

	ginkgo.Describe("Send", func() {
		ginkgo.It("should send a simple email", func() {
			m := mail.New(
				[]string{"recipient@example.com"},
				"Test Subject",
				"Test Body",
				"text/plain",
			).SetFrom("Sender", "sender@example.com").
				SetCredentials("127.0.0.1", smtpPort, "", "")

			err := m.Send(v1.ConnectionSMTP{})
			Expect(err).To(BeNil())

			messages := testBackend.GetMessages()
			Expect(messages).To(HaveLen(1))

			msg := messages[0]
			Expect(msg.From).To(Equal("sender@example.com"))
			Expect(msg.To).To(Equal([]string{"recipient@example.com"}))

			data := string(msg.Data)
			Expect(data).To(ContainSubstring("Subject: Test Subject"))
			Expect(data).To(ContainSubstring("Test Body"))
		})

		ginkgo.It("should send HTML email with correct content-type", func() {
			m := mail.New(
				[]string{"recipient@example.com"},
				"HTML Email",
				"<h1>Hello</h1><p>World</p>",
				"text/html",
			).SetFrom("Sender", "sender@example.com").
				SetCredentials("127.0.0.1", smtpPort, "", "")

			err := m.Send(v1.ConnectionSMTP{})
			Expect(err).To(BeNil())

			messages := testBackend.GetMessages()
			Expect(messages).To(HaveLen(1))

			data := string(messages[0].Data)
			Expect(data).To(ContainSubstring("Content-Type: text/html"))
			Expect(data).To(ContainSubstring("<h1>Hello</h1>"))
		})

		ginkgo.It("should send to multiple recipients", func() {
			recipients := []string{
				"alice@example.com",
				"bob@example.com",
				"charlie@example.com",
			}

			m := mail.New(
				recipients,
				"Group Email",
				"Hello everyone",
				"text/plain",
			).SetFrom("Sender", "sender@example.com").
				SetCredentials("127.0.0.1", smtpPort, "", "")

			err := m.Send(v1.ConnectionSMTP{})
			Expect(err).To(BeNil())

			messages := testBackend.GetMessages()
			Expect(messages).To(HaveLen(1))
			Expect(messages[0].To).To(Equal(recipients))
		})

		ginkgo.It("should include custom headers", func() {
			m := mail.New(
				[]string{"recipient@example.com"},
				"Email with Headers",
				"Body",
				"text/plain",
			).SetFrom("Sender", "sender@example.com").
				SetCredentials("127.0.0.1", smtpPort, "", "").
				SetHeader("X-Custom-Header", "custom-value").
				SetHeader("X-Priority", "1")

			err := m.Send(v1.ConnectionSMTP{})
			Expect(err).To(BeNil())

			messages := testBackend.GetMessages()
			Expect(messages).To(HaveLen(1))

			data := string(messages[0].Data)
			Expect(data).To(ContainSubstring("X-Custom-Header: custom-value"))
			Expect(data).To(ContainSubstring("X-Priority: 1"))
		})

		ginkgo.It("should handle unicode in subject and body", func() {
			m := mail.New(
				[]string{"recipient@example.com"},
				"日本語の件名 - Unicode Subject",
				"Привет мир! 你好世界!",
				"text/plain; charset=utf-8",
			).SetFrom("发送者", "sender@example.com").
				SetCredentials("127.0.0.1", smtpPort, "", "")

			err := m.Send(v1.ConnectionSMTP{})
			Expect(err).To(BeNil())

			messages := testBackend.GetMessages()
			Expect(messages).To(HaveLen(1))

			// Parse the multipart message and extract the inline part
			entity, err := gomsg.Read(bytes.NewReader(messages[0].Data))
			Expect(err).To(BeNil())

			mr := entity.MultipartReader()
			Expect(mr).ToNot(BeNil())

			part, err := mr.NextPart()
			Expect(err).To(BeNil())

			body, err := io.ReadAll(part.Body)
			Expect(err).To(BeNil())

			bodyStr := string(body)
			Expect(bodyStr).To(ContainSubstring("Привет мир!"))
			Expect(bodyStr).To(ContainSubstring("你好世界!"))
		})

		ginkgo.It("should set from name correctly", func() {
			m := mail.New(
				[]string{"recipient@example.com"},
				"Test",
				"Body",
				"text/plain",
			).SetFrom("John Doe", "john@example.com").
				SetCredentials("127.0.0.1", smtpPort, "", "")

			err := m.Send(v1.ConnectionSMTP{})
			Expect(err).To(BeNil())

			messages := testBackend.GetMessages()
			Expect(messages).To(HaveLen(1))

			data := string(messages[0].Data)
			Expect(data).To(ContainSubstring("John Doe"))
			Expect(data).To(ContainSubstring("john@example.com"))
		})

		ginkgo.It("should fail when SMTP server is unreachable", func() {
			m := mail.New(
				[]string{"recipient@example.com"},
				"Test",
				"Body",
				"text/plain",
			).SetFrom("Sender", "sender@example.com").
				SetCredentials("127.0.0.1", 59999, "", "") // Invalid port

			err := m.Send(v1.ConnectionSMTP{})
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("connection refused"))
		})
	})

	ginkgo.Describe("buildMessage", func() {
		ginkgo.It("should include Date header", func() {
			m := mail.New(
				[]string{"recipient@example.com"},
				"Test",
				"Body",
				"text/plain",
			).SetFrom("Sender", "sender@example.com").
				SetCredentials("127.0.0.1", smtpPort, "", "")

			err := m.Send(v1.ConnectionSMTP{})
			Expect(err).To(BeNil())

			messages := testBackend.GetMessages()
			Expect(messages).To(HaveLen(1))

			data := string(messages[0].Data)
			Expect(data).To(MatchRegexp(`Date: .+`))
		})

		ginkgo.It("should properly format To header with multiple recipients", func() {
			m := mail.New(
				[]string{"a@example.com", "b@example.com"},
				"Test",
				"Body",
				"text/plain",
			).SetFrom("Sender", "sender@example.com").
				SetCredentials("127.0.0.1", smtpPort, "", "")

			err := m.Send(v1.ConnectionSMTP{})
			Expect(err).To(BeNil())

			messages := testBackend.GetMessages()
			Expect(messages).To(HaveLen(1))

			data := string(messages[0].Data)
			// Check that To header contains both addresses
			lines := strings.Split(data, "\n")
			var toHeader string
			for _, line := range lines {
				if strings.HasPrefix(line, "To:") {
					toHeader = line
					break
				}
			}
			Expect(toHeader).To(ContainSubstring("a@example.com"))
			Expect(toHeader).To(ContainSubstring("b@example.com"))
		})
	})
})
