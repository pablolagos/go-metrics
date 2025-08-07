package go_metrics

import (
	"encoding/json"
	"fmt"
	"log"
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
	now          func() time.Time // Current time for reference
}

var ErrCorruptedFile = fmt.Errorf("corrupted metrics file")

// NewMetricsManager initializes the MetricsManager with persistence settings.
func NewMetricsManager(filePath string, maxDays int, saveInterval time.Duration) (*MetricsManager, error) {
	mm := &MetricsManager{
		metrics:      make(map[time.Time]*Metric),
		filePath:     filePath,
		maxDays:      maxDays,
		saveInterval: saveInterval,
		now:          time.Now, // Default to the real current time
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
func (mm *MetricsManager) GetMetricsForLastDays(days int, loc *time.Location) ([]Metric, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	// 1) Compute today's midnight in the local timezone
	nowLocal := mm.now().In(loc)
	todayMidnight := time.Date(
		nowLocal.Year(), nowLocal.Month(), nowLocal.Day(),
		0, 0, 0, 0,
		loc,
	)

	// 2) Build slice of local midnights for each of the last 'days'
	localDays := make([]time.Time, days)
	for i := 0; i < days; i++ {
		// localDays[0] = today - (days-1), ..., localDays[days-1] = today
		localDays[days-1-i] = todayMidnight.AddDate(0, 0, -i)
	}

	// 3) Initialize result array with zeroed counters
	result := make([]Metric, days)
	for i, dayStart := range localDays {
		result[i] = Metric{
			Date:     dayStart,
			Counters: mm.initializeZeroCounters(),
		}
	}

	// 4) Aggregate existing metrics
	for utcKey, mtr := range mm.metrics {
		// Convert the UTC-keyed timestamp into local time
		localTs := utcKey.In(loc)
		// Truncate to local midnight
		dayStart := time.Date(
			localTs.Year(), localTs.Month(), localTs.Day(),
			0, 0, 0, 0,
			loc,
		)
		// Find matching slot and accumulate
		for i, ds := range localDays {
			if ds.Equal(dayStart) {
				for k, cnt := range mtr.Counters {
					log.Printf("Adding %d to %s on %s", cnt.Load(), k, ds)
					result[i].Counters[k].Add(cnt.Load())
				}
				break
			}
		}
	}

	return result, nil
}

// GetMetricsForLastHours retrieves metrics for the last `hours` in the specified timezone.
func (mm *MetricsManager) GetMetricsForLastHours(hours int, loc *time.Location) ([]Metric, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	// 1) Compute start of the current hour in local time
	nowLocal := time.Now().In(loc)
	currentLocalHour := time.Date(
		nowLocal.Year(), nowLocal.Month(), nowLocal.Day(),
		nowLocal.Hour(), 0, 0, 0,
		loc,
	)

	// 2) Build slice of local hour starts
	localHours := make([]time.Time, hours)
	for i := 0; i < hours; i++ {
		localHours[hours-1-i] = currentLocalHour.Add(
			-time.Duration(i) * time.Hour,
		)
	}

	// 3) Prepare results, initialize zeros
	result := make([]Metric, 0, hours)
	for _, hourStart := range localHours {
		// Convert to UTC key for lookup
		keyUTC := hourStart.UTC()
		metric, exists := mm.metrics[keyUTC]
		if !exists {
			// create empty
			metric = &Metric{
				Date:     hourStart, // keep in local
				Counters: mm.initializeZeroCounters(),
			}
		} else {
			// fill missing counters
			for k := range mm.getAllKnownKeys() {
				if _, ok := metric.Counters[k]; !ok {
					metric.Counters[k] = &AtomicInt64{}
				}
			}
			// override Date to local hour
			metric.Date = hourStart
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
