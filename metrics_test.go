package go_metrics

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGetMetricsForLastDays verifies that GetMetricsForLastDays correctly aggregates metrics per local day.
func TestGetMetricsForLastDays(t *testing.T) {
	tests := []struct {
		name           string
		metrics        map[time.Time]*Metric
		days           int
		location       *time.Location
		expectedLength int
		expectedCounts map[time.Time]*Metric
	}{
		{
			name:           "single metric single day",
			metrics:        generateMetrics([][2]string{{"2025-08-06T00:00:00Z", "key1|5"}}),
			days:           1,
			location:       time.UTC,
			expectedLength: 1,
			expectedCounts: generateMetrics([][2]string{{"2025-08-06T00:00:00Z", "key1|5"}}),
		},
		{
			name: "multiple metrics single day",
			metrics: generateMetrics([][2]string{
				{"2025-08-06T00:00:00Z", "key1|5,key2|10"},
				{"2025-08-06T01:00:00Z", "key1|1,key3|20"},
				{"2025-08-06T02:00:00Z", "key1|15,key3|8"},
			}),
			days:           1,
			location:       time.UTC,
			expectedLength: 1,
			expectedCounts: generateMetrics([][2]string{
				{"2025-08-06T00:00:00Z", "key1|21,key2|10,key3|28"},
			}),
		},
		{
			name:           "no metrics in range",
			metrics:        generateMetrics([][2]string{}),
			days:           3,
			location:       time.UTC,
			expectedLength: 3,
			expectedCounts: generateMetrics([][2]string{}),
		},
		{
			name: "multiple metrics multiple days",
			metrics: generateMetrics([][2]string{
				// Day 1 metrics (2025-08-04)
				{"2025-08-04T05:00:00Z", "key1|2,key2|3"},
				{"2025-08-04T18:00:00Z", "key1|8"},
				{"2025-08-04T23:59:59Z", "key2|7"},
				// Day 2 metrics (2025-08-05)
				{"2025-08-05T03:00:00Z", "key1|6"},
				{"2025-08-05T15:00:00Z", "key2|4,key3|10"},
				// Day 3 metrics (2025-08-06)
				{"2025-08-06T00:00:00Z", "key1|1"},
				{"2025-08-06T22:00:00Z", "key3|5"},
			}),
			days:           3,
			location:       time.UTC,
			expectedLength: 3,
			expectedCounts: generateMetrics([][2]string{
				{"2025-08-04T00:00:00Z", "key1|10,key2|10,key3|0"},
				{"2025-08-05T00:00:00Z", "key1|6,key2|4,key3|10"},
				{"2025-08-06T00:00:00Z", "key1|1,key3|5,key2|0"},
			}),
		},
		{
			name: "metrics across timezones",
			metrics: generateMetrics([][2]string{
				{"2025-08-04T23:00:00Z", "key1|5"},
			}),
			days:           1,
			location:       time.FixedZone("PST", -8*60*60),
			expectedLength: 1,
			expectedCounts: generateMetrics([][2]string{
				{"2025-08-06 00:00:00 -0800 PST", "key1|5"},
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := MetricsManager{
				metrics: tt.metrics,
				now:     func() time.Time { return time.Date(2025, 8, 6, 0, 0, 0, 0, time.UTC) },
			}
			result, err := mm.GetMetricsForLastDays(tt.days, tt.location)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result) != tt.expectedLength {
				t.Errorf("expected %d metrics, got %d", tt.expectedLength, len(result))
			}

			for _, res := range result {
				_, ok := tt.expectedCounts[res.Date]
				require.True(t, ok, "expected counts for date %s to be non-empty", res.Date)
				require.Equal(t, len(tt.expectedCounts[res.Date].Counters), len(res.Counters), "expected counters length mismatch for date %s", res.Date)
				for key, expectedCounter := range tt.expectedCounts[res.Date].Counters {
					counter, exists := res.Counters[key]
					require.True(t, exists, "expected counter for key %s to exist on date %s", key, res.Date)
					require.Equal(t, expectedCounter.Load(), counter.Load(), "expected counter value mismatch for key %s on date %s", key, res.Date)
				}

			}
		})
	}
}

func generateMetrics(data [][2]string) map[time.Time]*Metric {
	metrics := make(map[time.Time]*Metric)
	for _, d := range data {
		ts, _ := time.Parse(time.RFC3339, d[0])
		counters := make(map[string]*AtomicInt64)
		for _, pair := range stringSplit(d[1], ",") {
			kv := stringSplit(pair, "|")
			key := kv[0]
			val := toInt64(kv[1])
			counters[key] = &AtomicInt64{}
			counters[key].Add(val)
		}
		metrics[ts] = &Metric{
			Date:     ts,
			Counters: counters,
		}
	}
	return metrics
}

// stringSplit splits the input string by the given separator and returns the non-empty trimmed parts.
func stringSplit(input, sep string) []string {
	parts := strings.Split(input, sep)
	var out []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// toInt64 converts a string to int64, panicking on error (suitable for tests/utilities).
func toInt64(s string) int64 {
	val, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		panic("invalid int64: " + s)
	}
	return val
}
