package clientcmd

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("playbook apply manifest", func() {
	ginkgo.It("converts a Playbook CRD into apply parameters", func() {
		params, err := parsePlaybookManifest([]byte(`
apiVersion: mission-control.flanksource.com/v1
kind: Playbook
metadata:
  name: restart
  namespace: ops
spec:
  title: Restart workload
  category: Kubernetes
  description: Restarts a workload
  actions:
    - name: echo
      exec:
        script: echo ok
`))

		Expect(err).ToNot(HaveOccurred())
		Expect(params.Namespace).To(Equal("ops"))
		Expect(params.Name).To(Equal("restart"))
		Expect(params.Spec).To(MatchJSON(`{"title":"Restart workload","category":"Kubernetes","description":"Restarts a workload","actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`))
	})

	ginkgo.It("defaults the namespace", func() {
		params, err := parsePlaybookManifest([]byte(`
kind: Playbook
metadata:
  name: diagnose
spec:
  actions:
    - name: echo
      exec:
        script: echo ok
`))

		Expect(err).ToNot(HaveOccurred())
		Expect(params.Namespace).To(Equal("default"))
	})

	ginkgo.It("leaves full schema validation to the server", func() {
		params, err := parsePlaybookManifest([]byte(`
kind: Playbook
metadata:
  name: invalid
spec:
  unexpected: true
  actions:
    - name: echo
      exec:
        script: echo ok
`))

		Expect(err).ToNot(HaveOccurred())
		Expect(params.Spec).To(MatchJSON(`{"unexpected":true,"actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`))
	})

	ginkgo.It("rejects other manifest kinds", func() {
		_, err := parsePlaybookManifest([]byte(`
kind: Connection
metadata:
  name: invalid
spec: {}
`))

		Expect(err).To(MatchError(`manifest kind must be Playbook, got "Connection"`))
	})
})
