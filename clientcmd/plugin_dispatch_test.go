package clientcmd

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/flanksource/clicky/rpc"
	"github.com/flanksource/incident-commander/sdk"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("plugin dispatch server error formatting", func() {
	ginkgo.It("formats structured oops server errors without dumping raw JSON", func() {
		err := &sdk.ServerError{
			StatusCode: http.StatusInternalServerError,
			Body:       []byte(`{"code":"HANDLER_ERROR","error":"config_item_id is required","stacktrace":"Oops: config_item_id is required\n  --- at controller.go:165 InvokeOperation()"}`),
			Code:       "HANDLER_ERROR",
			Message:    "config_item_id is required",
			Trace:      "01KR0Z7BNP92W83AEQ1MMATATA",
			Time:       "2026-05-07T10:20:07.73204Z",
			Context: map[string]any{
				"user":      "Admin",
				"name":      "",
				"namespace": "",
			},
			Stacktrace: "Oops: config_item_id is required\n  --- at controller.go:165 InvokeOperation()",
		}

		formatted := formatPluginServerError(err)

		Expect(formatted).To(ContainSubstring("server 500"))
		Expect(formatted).To(ContainSubstring("Code: HANDLER_ERROR"))
		Expect(formatted).To(ContainSubstring("Error: config_item_id is required"))
		Expect(formatted).To(ContainSubstring("Trace: 01KR0Z7BNP92W83AEQ1MMATATA"))
		Expect(formatted).To(ContainSubstring("Time: 2026-05-07T10:20:07.73204Z"))
		Expect(formatted).To(ContainSubstring("Context:\n  name: \n  namespace: \n  user: Admin"))
		Expect(formatted).To(ContainSubstring("Stacktrace:\n  Oops: config_item_id is required\n    --- at controller.go:165 InvokeOperation()"))
		Expect(formatted).NotTo(ContainSubstring(`{"code":"HANDLER_ERROR"`))
		Expect(formatted).NotTo(ContainSubstring(`\n  --- at controller.go:165`))
	})
})

var _ = ginkgo.Describe("plugin operation input validation", func() {
	ginkgo.It("requires config id for config-scoped operations without printing usage", func() {
		called := false
		cmd := newOperationCommandWithDispatcher("arthas", rpc.RPCOperation{
			Name: "session-create",
			Tags: []string{"config"},
		}, func(*cobra.Command, string, string, map[string]string, string, bool) error {
			called = true
			return nil
		})

		_, errOut, err := executeCommand(cmd)

		Expect(err).To(MatchError(`--config-id is required for config-scoped operation "session-create"`))
		Expect(errOut).NotTo(ContainSubstring("Usage:"))
		Expect(called).To(BeFalse())
	})

	ginkgo.It("requires manifest-declared parameters before dispatch", func() {
		called := false
		cmd := newOperationCommandWithDispatcher("logs", rpc.RPCOperation{
			Name: "tail",
			Tags: []string{"config"},
			Parameters: []rpc.RPCParameter{
				{Name: "namespace", Required: true},
				{Name: "podName", Required: true},
				{Name: "tailLines"},
			},
		}, func(*cobra.Command, string, string, map[string]string, string, bool) error {
			called = true
			return nil
		})

		_, errOut, err := executeCommand(cmd, "--config-id", "config-1", "--param", "namespace= ")

		Expect(err).To(MatchError("missing required parameter(s): namespace, podName; pass with --param key=value"))
		Expect(errOut).NotTo(ContainSubstring("Usage:"))
		Expect(called).To(BeFalse())
	})

	ginkgo.It("dispatches when config id and required parameters are present", func() {
		called := false
		cmd := newOperationCommandWithDispatcher("logs", rpc.RPCOperation{
			Name: "tail",
			Tags: []string{"config"},
			Parameters: []rpc.RPCParameter{
				{Name: "podName", Required: true},
			},
		}, func(_ *cobra.Command, plugin, op string, params map[string]string, configID string, raw bool) error {
			called = true
			Expect(plugin).To(Equal("logs"))
			Expect(op).To(Equal("tail"))
			Expect(configID).To(Equal("config-1"))
			Expect(raw).To(BeTrue())
			Expect(params).To(HaveKeyWithValue("podName", "api"))
			return nil
		})

		_, errOut, err := executeCommand(cmd, "--config-id", "config-1", "--param", "podName=api", "--json")

		Expect(err).NotTo(HaveOccurred())
		Expect(errOut).To(BeEmpty())
		Expect(called).To(BeTrue())
	})

	ginkgo.It("does not append usage to formatted server errors", func() {
		serverErr := &sdk.ServerError{
			StatusCode: http.StatusInternalServerError,
			Code:       "HANDLER_ERROR",
			Message:    "config_item_id is required",
		}
		cmd := newOperationCommandWithDispatcher("arthas", rpc.RPCOperation{
			Name: "session-create",
		}, func(*cobra.Command, string, string, map[string]string, string, bool) error {
			return fmt.Errorf("forward to http://localhost:8080: %s", formatPluginServerError(serverErr))
		})

		_, errOut, err := executeCommand(cmd)

		Expect(err).To(MatchError(ContainSubstring("Code: HANDLER_ERROR")))
		Expect(err).To(MatchError(ContainSubstring("Error: config_item_id is required")))
		Expect(errOut).NotTo(ContainSubstring("Usage:"))
	})
})

func executeCommand(cmd *cobra.Command, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if args == nil {
		args = []string{}
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}
