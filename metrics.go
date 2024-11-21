package go_metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"
)

type Metric struct {
	Date     time.Time               `json:"date"`
	Counters map[string]*AtomicInt64 `json:"counters"`
}

type MetricsManager struct {
	metrics      map[time.Time]*Metric // Store metrics by date or hour
	mu           sync.RWMutex          // Read-Write lock for managing the map
	filePath     string
	maxDays      int
	saveInterval time.Duration
}

// NewMetricsManager initializes the MetricsManager with persistence settings.
func NewMetricsManager(filePath string, maxDays int, saveInterval time.Duration) *MetricsManager {
	mm := &MetricsManager{
		metrics:      make(map[time.Time]*Metric),
		filePath:     filePath,
		maxDays:      maxDays,
		saveInterval: saveInterval,
	}

	// Attempt to load metrics from the file system on initialization.
	err := mm.loadMetrics()
	if err != nil {
		fmt.Printf("Warning: Unable to load metrics from file. Starting fresh. Error: %v\n", err)
	}

	// Start periodic saving of metrics to ensure persistence.
	go mm.startSaving()

	// Set up signal handling to save metrics on process termination.
	go mm.handleSignals()

	return mm
}

// Increment increases the value of a specified metric atomically.
func (mm *MetricsManager) Increment(metricKey string) {
	mm.IncrementBy(metricKey, 1)
}

// Decrement decreases the value of a specified metric atomically.
func (mm *MetricsManager) Decrement(metricKey string) {
	mm.DecrementBy(metricKey, 1)
}

// IncrementBy increases the value of a specified metric by a given amount atomically.
func (mm *MetricsManager) IncrementBy(metricKey string, value int64) {
	mm.getCurrentMetric().incrementBy(metricKey, value)
}

// DecrementBy decreases the value of a specified metric by a given amount atomically.
func (mm *MetricsManager) DecrementBy(metricKey string, value int64) {
	mm.getCurrentMetric().incrementBy(metricKey, -value)
}

// getCurrentMetric ensures there is a Metric for the current hour and returns it.
func (mm *MetricsManager) getCurrentMetric() *Metric {
	now := time.Now().UTC().Truncate(time.Hour)

	mm.mu.RLock()
	metric, exists := mm.metrics[now]
	mm.mu.RUnlock()

	if exists {
		return metric
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	metric, exists = mm.metrics[now]
	if !exists {
		metric = &Metric{
			Date:     now,
			Counters: make(map[string]*AtomicInt64),
		}
		mm.metrics[now] = metric
		mm.cleanupOldMetrics()
	}

	return metric
}

// incrementBy increases the specified counter by a given value atomically.
func (m *Metric) incrementBy(key string, delta int64) {
	m.ensureCounterExists(key)
	m.Counters[key].Add(delta)
}

// ensureCounterExists checks if a counter exists, and initializes it to zero if it doesn't.
func (m *Metric) ensureCounterExists(key string) {
	if _, exists := m.Counters[key]; !exists {
		m.Counters[key] = &AtomicInt64{}
	}
}

// cleanupOldMetrics removes metrics that are older than the configured maxDays.
func (mm *MetricsManager) cleanupOldMetrics() {
	cutoff := time.Now().UTC().Add(-time.Duration(mm.maxDays*24) * time.Hour)
	for date := range mm.metrics {
		if date.Before(cutoff) {
			delete(mm.metrics, date)
		}
	}
}

// loadMetrics loads metrics from a JSON file on disk.
func (mm *MetricsManager) loadMetrics() error {
	file, err := os.Open(mm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			mm.metrics = make(map[time.Time]*Metric)
			return nil
		}
		return err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&mm.metrics); err != nil {
		mm.metrics = make(map[time.Time]*Metric)
		return fmt.Errorf("failed to decode metrics file: %v", err)
	}
	return nil
}

// saveMetrics saves metrics to a JSON file on disk.
func (mm *MetricsManager) saveMetrics() error {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	file, err := os.Create(mm.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	return encoder.Encode(mm.metrics)
}

// startSaving periodically saves metrics to the file system.
func (mm *MetricsManager) startSaving() {
	ticker := time.NewTicker(mm.saveInterval)
	defer ticker.Stop()

	for range ticker.C {
		err := mm.saveMetrics()
		if err != nil {
			fmt.Printf("Error saving metrics: %v\n", err)
		}
	}
}

// handleSignals intercepts OS signals to save metrics before process termination.
func (mm *MetricsManager) handleSignals() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for sig := range signalChan {
		fmt.Printf("Received signal: %s, saving metrics...\n", sig)
		err := mm.saveMetrics()
		if err != nil {
			fmt.Printf("Error saving metrics on signal: %v\n", err)
		}
	}
}

// GetMetricsForLastDays retrieves metrics for the last `days` in the specified timezone.
func (mm *MetricsManager) GetMetricsForLastDays(days int, location *time.Location) ([]Metric, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	now := time.Now().In(location)
	cutoff := now.AddDate(0, 0, -days).Truncate(24 * time.Hour)

	var result []Metric
	for date, metric := range mm.metrics {
		metricTime := date.In(location).Truncate(24 * time.Hour)
		if metricTime.After(cutoff) || metricTime.Equal(cutoff) {
			result = append(result, *metric)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Date.Before(result[j].Date)
	})

	return result, nil
}

// GetMetricsForLastHours retrieves metrics for the last `hours` in the specified timezone.
func (mm *MetricsManager) GetMetricsForLastHours(hours int, location *time.Location) ([]Metric, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	now := time.Now().In(location)
	cutoff := now.Add(-time.Duration(hours) * time.Hour)

	var result []Metric
	for date, metric := range mm.metrics {
		metricTime := date.In(location)
		if metricTime.After(cutoff) || metricTime.Equal(cutoff) {
			result = append(result, *metric)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Date.Before(result[j].Date)
	})

	return result, nil
}
