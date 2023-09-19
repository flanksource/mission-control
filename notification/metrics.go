package notification

import "github.com/prometheus/client_golang/prometheus"

func init() {
	prometheus.MustRegister(notificationSentCounter, notificationSendDuration)
}

var (
	notificationSentCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "sent_total",
			Subsystem: "notification",
			Help:      "Total number of notifications sent",
		},
		[]string{"service"},
	)

	notificationSendDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:      "send_duration_seconds",
		Subsystem: "notification",
		Help:      "Duration to send a notification.",
	}, []string{"service"})
)
