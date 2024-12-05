package go_metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
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

var ErrCorruptedFile = fmt.Errorf("corrupted metrics file")

// NewMetricsManager initializes the MetricsManager with persistence settings.
func NewMetricsManager(filePath string, maxDays int, saveInterval time.Duration) (*MetricsManager, error) {
	mm := &MetricsManager{
		metrics:      make(map[time.Time]*Metric),
		filePath:     filePath,
		maxDays:      maxDays,
		saveInterval: saveInterval,
	}

	// Attempt to load metrics from the file system on initialization.
	err := mm.loadMetrics()
	if err != nil {
		return nil, fmt.Errorf("failed to load metrics: %v", err)
	}

	// Start periodic saving of metrics to ensure persistence.
	go mm.startSaving()

	return mm, nil
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
	// create file if not exists
	if _, err := os.Stat(mm.filePath); os.IsNotExist(err) {
		mm.metrics = make(map[time.Time]*Metric)
		err := mm.SaveMetrics()
		if err != nil {
			return fmt.Errorf("cannot create metrics file: %v", err)
		}
		return nil
	}

	// open and read file
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
		return fmt.Errorf("failed to decode metrics file: %v", ErrCorruptedFile)
	}
	return nil
}

// SaveMetrics saves metrics to a JSON file on disk.
func (mm *MetricsManager) SaveMetrics() error {
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
		err := mm.SaveMetrics()
		if err != nil {
			fmt.Printf("Error saving metrics: %v\n", err)
		}
	}
}

// GetMetricsForLastDays retrieves metrics for the last `days` in the specified timezone.
func (mm *MetricsManager) GetMetricsForLastDays(days int, location *time.Location) ([]Metric, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	now := time.Now().In(location).Truncate(24 * time.Hour)
	cutoff := now.AddDate(0, 0, -days)

	// Map to store aggregated daily metrics
	dailyMetrics := make(map[time.Time]*Metric)

	// Aggregate metrics by day
	for metricDate, metric := range mm.metrics {
		// Convert the metric date to the requested timezone and truncate to the day
		metricDay := metricDate.In(location).Truncate(24 * time.Hour)

		// Ignore metrics outside the range
		if metricDay.Before(cutoff) || metricDay.After(now) {
			continue
		}

		// Initialize daily metric if not already present
		if _, exists := dailyMetrics[metricDay]; !exists {
			dailyMetrics[metricDay] = &Metric{
				Date:     metricDay,
				Counters: mm.initializeZeroCounters(), // Initialize all known counters to 0
			}
		}

		// Aggregate counters for the day
		for key, counter := range metric.Counters {
			dailyMetrics[metricDay].Counters[key].Add(counter.Load())
		}
	}

	// Fill missing days with zeroed metrics
	result := make([]Metric, 0, days)
	for i := 0; i < days; i++ {
		day := cutoff.AddDate(0, 0, i)

		if dailyMetric, exists := dailyMetrics[day]; exists {
			result = append(result, *dailyMetric)
		} else {
			// Create a zeroed metric for missing days
			result = append(result, Metric{
				Date:     day.UTC(),
				Counters: mm.initializeZeroCounters(),
			})
		}
	}

	return result, nil
}

// GetMetricsForLastHours retrieves metrics for the last `hours` in the specified timezone.
func (mm *MetricsManager) GetMetricsForLastHours(hours int, location *time.Location) ([]Metric, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	now := time.Now().In(location).Truncate(time.Hour)
	cutoff := now.Add(-time.Duration(hours) * time.Hour)

	// Create a complete list of metrics with counters initialized to 0
	result := make([]Metric, 0, hours)
	for i := 0; i < hours; i++ {
		date := cutoff.Add(time.Duration(i) * time.Hour)
		metricDate := date.UTC()

		metric, exists := mm.metrics[metricDate]
		if !exists {
			// Create an empty metric with all counters initialized to 0
			metric = &Metric{
				Date:     metricDate,
				Counters: mm.initializeZeroCounters(),
			}
		} else {
			// Fill missing counters with values initialized to 0
			for key := range mm.getAllKnownKeys() {
				if _, ok := metric.Counters[key]; !ok {
					metric.Counters[key] = &AtomicInt64{}
				}
			}
		}

		result = append(result, *metric)
	}

	return result, nil
}

func (mm *MetricsManager) initializeZeroCounters() map[string]*AtomicInt64 {
	counters := make(map[string]*AtomicInt64)
	for key := range mm.getAllKnownKeys() {
		counters[key] = &AtomicInt64{}
	}
	return counters
}

func (mm *MetricsManager) getAllKnownKeys() map[string]struct{} {
	keys := make(map[string]struct{})
	for _, metric := range mm.metrics {
		for key := range metric.Counters {
			keys[key] = struct{}{}
		}
	}
	return keys
}
