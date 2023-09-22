package notification

import "github.com/prometheus/client_golang/prometheus"

func init() {
	prometheus.MustRegister(notificationSentCounter, notificationSendFailureCounter, notificationSendDuration)
}

var (
	notificationSentCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "sent_total",
			Subsystem: "notification",
			Help:      "Total number of notifications sent",
		},
		[]string{"service", "recipient_type", "id"},
	)

	notificationSendFailureCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "send_error_total",
			Subsystem: "notification",
			Help:      "Total number of failure notifications sent",
		},
		[]string{"service", "recipient_type", "id"},
	)

	notificationSendDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:      "send_duration_seconds",
		Subsystem: "notification",
		Help:      "Duration to send a notification.",
	}, []string{"service", "recipient_type", "id"})
)
