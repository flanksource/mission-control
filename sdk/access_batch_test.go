package sdk

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testAccessQueryLimit = 6000

var _ = ginkgo.Describe("access query batching", func() {
	userIDs := make([]string, 400)
	for i := range userIDs {
		userIDs[i] = fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1)
	}

	for _, tt := range []struct {
		name string
		call func(*Client) error
	}{
		{
			name: "permissions",
			call: func(client *Client) error {
				_, _, err := client.ListAccessGrants(context.Background(), AccessGrantOptions{UserIDs: userIDs})
				return err
			},
		},
		{
			name: "config permission rollups",
			call: func(client *Client) error {
				_, _, err := client.ListAccessSummaryByConfig(context.Background(), AccessGrantOptions{ConfigIDs: userIDs})
				return err
			},
		},
		{
			name: "logs",
			call: func(client *Client) error {
				_, _, err := client.ListAccessLogs(context.Background(), AccessHistoryOptions{UserIDs: userIDs})
				return err
			},
		},
		{
			name: "reviews",
			call: func(client *Client) error {
				_, _, err := client.ListAccessReviews(context.Background(), AccessHistoryOptions{UserIDs: userIDs})
				return err
			},
		},
	} {
		ginkgo.It("keeps "+tt.name+" requests below the proxy URL limit", func() {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if len(r.URL.RawQuery) > testAccessQueryLimit {
					w.Header().Set("Content-Type", "text/html")
					w.WriteHeader(http.StatusRequestURITooLong)
					_, _ = w.Write([]byte("<html>URI too long</html>"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[]"))
			}))
			defer server.Close()

			Expect(tt.call(New(server.URL, "tok"))).To(Succeed())
			Expect(requests.Load()).To(BeNumerically(">", 1))
		})
	}

	ginkgo.It("merges batch totals before applying the global order and limit", func() {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request := requests.Add(1)
			Expect(len(r.URL.RawQuery)).To(BeNumerically("<=", testAccessQueryLimit))
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Range", "0-0/2")
			_, _ = fmt.Fprintf(w, `[{"config_id":%q,"config_name":"config","external_user_id":%q,"user":"user-%02d","role":"Reader"}]`, config1, userOne, 100-request)
		}))
		defer server.Close()

		grants, total, err := New(server.URL, "tok").ListAccessGrants(context.Background(), AccessGrantOptions{
			UserIDs: userIDs,
			Limit:   1,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(requests.Load()).To(BeNumerically(">", 1))
		Expect(total).To(Equal(int(requests.Load()) * 2))
		Expect(grants).To(HaveLen(1))
		Expect(grants[0].User).To(Equal(fmt.Sprintf("user-%02d", 100-requests.Load())))
	})

	ginkgo.It("pages past the server response cap up to the requested limit", func() {
		var offsets []int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
			if r.URL.Query().Get("offset") == "" {
				offset = 0
				err = nil
			}
			Expect(err).ToNot(HaveOccurred())
			offsets = append(offsets, offset)
			w.Header().Set("Content-Type", "application/json")
			if offset == 0 {
				w.Header().Set("Content-Range", "0-1/3")
				_, _ = fmt.Fprintf(w, `[
					{"config_id":%q,"config_name":"config","external_user_id":%q,"user":"alpha","role":"Reader"},
					{"config_id":%q,"config_name":"config","external_user_id":%q,"user":"bravo","role":"Reader"}
				]`, config1, userOne, config1, userOne)
				return
			}
			Expect(offset).To(Equal(2))
			w.Header().Set("Content-Range", "2-2/3")
			_, _ = fmt.Fprintf(w, `[{"config_id":%q,"config_name":"config","external_user_id":%q,"user":"charlie","role":"Reader"}]`, config1, userOne)
		}))
		defer server.Close()

		grants, total, err := New(server.URL, "tok").ListAccessGrants(context.Background(), AccessGrantOptions{Limit: 10_000})

		Expect(err).ToNot(HaveOccurred())
		Expect(total).To(Equal(3))
		Expect(grants).To(HaveLen(3))
		Expect(offsets).To(Equal([]int{0, 2}))
	})
})
