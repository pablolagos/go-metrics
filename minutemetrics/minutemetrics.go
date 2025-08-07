package minutemetrics

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// Metric represents counters for a specific minute.
type Metric struct {
	Date     time.Time
	Counters map[string]*atomic.Uint64
}

type MetricResponse struct {
	Date     time.Time
	Counters map[string]uint64
}

// TimeSeriesPoint represents a single data point in a time series response.
type TimeSeriesPoint struct {
	Time  time.Time
	Value uint64
}

// TimeSeriesResponse is a collection of time-based metric data points.
type TimeSeriesResponse []TimeSeriesPoint

// MinuteMetricManager maintains only the current minute's Metric in memory,
// flushing it to Badger once the minute elapses, and cleans up old data.
type MinuteMetricManager struct {
	db            *badger.DB
	mu            sync.RWMutex
	maxHours      int
	cleanInterval time.Duration
	quit          chan struct{}
	lastMetric    *Metric
	lastTimestamp time.Time
}

// New creates and initializes a new MinuteMetricManager with the specified maximum number of hours for metric retention.
// The dbFolder must be for exclusive use by this manager.
// It starts background tasks for flushing and cleaning metrics stored in the Badger database.
func New(dbFolder string, maxHours int, cleanInterval time.Duration) (*MinuteMetricManager, error) {
	// Create a badger database optimized for small stats
	opts := badger.DefaultOptions(dbFolder).
		WithLogger(nil).
		// MemTableSize: maximum in-memory table size before flush, now 8 MiB instead of 32 MiB
		WithMemTableSize(8 << 20).
		// BaseTableSize: maximum SSTable size at level 1, now 8 MiB instead of 32 MiB
		WithBaseTableSize(8 << 20).
		// ValueLogFileSize: rotate value-log files at 64 MiB instead of 128 MiB
		WithValueLogFileSize(64 << 20).
		// LevelSizeMultiplier: growth factor between LSM levels (kept at 5)
		WithLevelSizeMultiplier(5).
		// NumMemtables: allow only 2 memtables in memory before stall
		WithNumMemtables(2).
		// NumLevelZeroTables: trigger compaction once there are 2 L0 SSTables
		WithNumLevelZeroTables(2).
		// NumLevelZeroTablesStall: stall writes if 3 L0 SSTables accumulate
		WithNumLevelZeroTablesStall(3)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open metrics database: %w", err)
	}

	// Create the metrics manager with the provided DB and configuration.
	mm := &MinuteMetricManager{db: db, maxHours: maxHours, cleanInterval: cleanInterval, quit: make(chan struct{})}

	// Initialize lastMetric for current minute
	mm.lastTimestamp = time.Now().UTC().Truncate(time.Minute)
	mm.lastMetric = mm.loadMetricFromDB(mm.lastTimestamp)
	if mm.lastMetric == nil {
		mm.lastMetric = &Metric{Date: mm.lastTimestamp, Counters: make(map[string]*atomic.Uint64)}
	}

	// Start background tasks
	go mm.startFlusher()
	go mm.startCleanup()
	return mm, nil
}

// Close stops background tasks and closes the DB.
func (mm *MinuteMetricManager) Close() error {
	close(mm.quit)
	return mm.db.Close()
}

// Increment adds 1 to the named counter in the current minute.
func (mm *MinuteMetricManager) Increment(key string) {
	mm.IncrementBy(key, 1)
}

// Decrement subtracts 1 from the named counter in the current minute.
func (mm *MinuteMetricManager) Decrement(key string) {
	mm.IncrementBy(key, -1)
}

// IncrementBy atomically adjusts the counter in memory; flushes happen at the minute boundary.
func (mm *MinuteMetricManager) IncrementBy(key string, delta int64) {
	m := mm.getCurrentMetric()
	m.incrementBy(key, delta)
}

// Set establece el valor del contador identificado por key en el minuto actual.
func (mm *MinuteMetricManager) Set(key string, value uint64) {
	m := mm.getCurrentMetric()
	ctr := m.ensureCounterExists(key)
	ctr.Store(value)
}

// getCurrentMetric returns the in-memory metric for the current minute,
// flushing and rotating if the minute has changed.
func (mm *MinuteMetricManager) getCurrentMetric() *Metric {
	now := time.Now().UTC().Truncate(time.Minute)
	if now.Equal(mm.lastTimestamp) {
		return mm.lastMetric
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	if now.Equal(mm.lastTimestamp) {
		return mm.lastMetric
	}

	mm.flushLastLocked() // Guarda el minuto anterior en disco.
	mm.lastTimestamp = now

	// Ya no se necesita cargar desde DB porque no hay concurrencia ni escritoras múltiples
	newM := &Metric{Date: now, Counters: make(map[string]*atomic.Uint64)}
	mm.lastMetric = newM
	return mm.lastMetric
}

// startFlusher waits for the end of each minute, then flushes the metric.
func (mm *MinuteMetricManager) startFlusher() {
	for {
		now := time.Now().UTC()
		next := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-time.After(time.Until(next)):
			mm.mu.Lock()
			mm.flushLastLocked()
			mm.mu.Unlock()
		case <-mm.quit:
			return
		}
	}
}

// flushLastLocked writes the lastMetric to Badger. Caller holds mm.mu.
func (mm *MinuteMetricManager) flushLastLocked() {
	if mm.lastMetric == nil {
		return
	}
	err := mm.db.Update(func(txn *badger.Txn) error {
		val, err := encodeMetric(mm.lastMetric)
		if err != nil {
			return err
		}
		return txn.Set(encodeTimestamp(mm.lastMetric.Date), val)
	})
	if err != nil {
		log.Printf("flush error: %v", err)
	}
}

// startCleanup periodically removes entries older than maxHours.
func (mm *MinuteMetricManager) startCleanup() {
	// Initial cleanup before starting the ticker
	mm.cleanupOld()

	// Set up a ticker to run cleanup at the specified interval
	t := time.NewTicker(mm.cleanInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			mm.cleanupOld()
		case <-mm.quit:
			return
		}
	}
}

// cleanupOld deletes keys older than cutoff.
func (mm *MinuteMetricManager) cleanupOld() {
	cutoff := time.Now().UTC().Add(-time.Duration(mm.maxHours) * time.Hour).Truncate(time.Hour)
	err := mm.db.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			kt := decodeTimestamp(it.Item().KeyCopy(nil))
			if kt.Before(cutoff) {
				txn.Delete(it.Item().KeyCopy(nil))
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("cleanup error: %v", err)
	}
}

// --- internal helpers ---

// incrementBy adjusts the counter atomically.
func (m *Metric) incrementBy(key string, delta int64) {
	ctr := m.ensureCounterExists(key)
	if delta >= 0 {
		ctr.Add(uint64(delta))
	} else {
		sub := uint64(-delta)
		ctr.Add(^sub + 1)
	}
}

// ensureCounterExists returns or creates the atomic counter.
func (m *Metric) ensureCounterExists(key string) *atomic.Uint64 {
	if ctr, ok := m.Counters[key]; ok {
		return ctr
	}
	ctr := new(atomic.Uint64)
	m.Counters[key] = ctr
	return ctr
}

// loadMetricFromDB loads a single-minute metric from Badger.
func (mm *MinuteMetricManager) loadMetricFromDB(ts time.Time) *Metric {
	var m *Metric
	err := mm.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(encodeTimestamp(ts))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil
			}
			return err
		}
		val, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		m, err = decodeMetric(val)
		return err
	})
	if err != nil {
		log.Printf("load error: %v", err)
	}
	return m
}

// encodeTimestamp converts time to 8-byte big endian key.
func encodeTimestamp(t time.Time) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(t.Unix()))
	return buf
}

// decodeTimestamp reads 8-byte big endian key to time.
func decodeTimestamp(b []byte) time.Time {
	n := int64(binary.BigEndian.Uint64(b))
	return time.Unix(n, 0).UTC()
}

// encodeMetric serializes Metric to bytes.
func encodeMetric(m *Metric) ([]byte, error) {
	buf := &bytes.Buffer{}
	// timestamp
	if err := binary.Write(buf, binary.BigEndian, m.Date.Unix()); err != nil {
		return nil, err
	}
	// count
	count := uint32(len(m.Counters))
	if err := binary.Write(buf, binary.BigEndian, count); err != nil {
		return nil, err
	}
	// each counter
	for k, ctr := range m.Counters {
		kb := []byte(k)
		if err := binary.Write(buf, binary.BigEndian, uint32(len(kb))); err != nil {
			return nil, err
		}
		buf.Write(kb)
		if err := binary.Write(buf, binary.BigEndian, ctr.Load()); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// decodeMetric deserializes bytes to Metric.
func decodeMetric(data []byte) (*Metric, error) {
	buf := bytes.NewReader(data)
	var ts int64
	if err := binary.Read(buf, binary.BigEndian, &ts); err != nil {
		return nil, err
	}
	m := &Metric{Date: time.Unix(ts, 0).UTC(), Counters: make(map[string]*atomic.Uint64)}
	var count uint32
	if err := binary.Read(buf, binary.BigEndian, &count); err != nil {
		return nil, err
	}
	for i := uint32(0); i < count; i++ {
		var klen uint32
		binary.Read(buf, binary.BigEndian, &klen)
		kb := make([]byte, klen)
		buf.Read(kb)
		var val uint64
		binary.Read(buf, binary.BigEndian, &val)
		ctr := new(atomic.Uint64)
		ctr.Store(val)
		m.Counters[string(kb)] = ctr
	}
	return m, nil
}

// getAggregatedMetrics fetches and aggregates metrics over the specified slots (minutes, hours, days),
// and sets the Date field in the requested timezone.
func (mm *MinuteMetricManager) getAggregatedMetrics(n int, slot time.Duration, loc *time.Location) ([]MetricResponse, error) {
	now := time.Now().In(loc)
	results := make([]MetricResponse, n)

	// Calculate the local slot start time for each interval
	slots := make([]time.Time, n)
	for i := 0; i < n; i++ {
		slots[n-1-i] = now.Add(-slot * time.Duration(i)).Truncate(slot)
	}

	// Map from slot start time (UTC) to its index in results
	slotMap := make(map[int64]int)
	for idx, t := range slots {
		// Pre-create the result entry with Date in requested timezone
		results[idx] = MetricResponse{
			Date:     t,
			Counters: make(map[string]uint64),
		}

		var utcStart time.Time
		switch slot {
		case time.Minute:
			// Compute the exact UTC instant for the start of this minute
			localStart := time.Date(
				t.Year(), t.Month(), t.Day(),
				t.Hour(), t.Minute(), 0, 0,
				loc,
			)
			utcStart = localStart.UTC()
		case time.Hour:
			// Compute the exact UTC instant for the start of this hour
			localHourStart := time.Date(
				t.Year(), t.Month(), t.Day(),
				t.Hour(), 0, 0, 0,
				loc,
			)
			utcStart = localHourStart.UTC()
		case 24 * time.Hour:
			// Compute the exact UTC instant for the start of this day (midnight local)
			localMidnight := time.Date(
				t.Year(), t.Month(), t.Day(),
				0, 0, 0, 0,
				loc,
			)
			utcStart = localMidnight.UTC()
		default:
			// Fallback: use UTC-truncated time
			utcStart = t.UTC().Truncate(slot)
		}

		slotMap[utcStart.Unix()] = idx
	}

	// Define the UTC range to scan: from start of first slot to end of last slot
	startUTC := slots[0].UTC()
	endLocal := slots[len(slots)-1].Add(slot)
	endUTC := endLocal.UTC()

	// Use Badger iterator in reverse (descending) order
	opts := badger.DefaultIteratorOptions
	opts.Reverse = true

	err := mm.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(opts)
		defer it.Close()
		// Seek just before endUTC
		it.Seek(encodeTimestamp(endUTC.Add(-time.Second)))

		for ; it.Valid(); it.Next() {
			item := it.Item()
			metricTime := decodeTimestamp(item.KeyCopy(nil))

			// Stop once before the start of our range
			if metricTime.Before(startUTC) {
				break
			}

			// Determine which slot this metricTime belongs to
			var slotKey int64
			switch slot {
			case time.Minute:
				slotKey = metricTime.Truncate(time.Minute).Unix()
			case time.Hour:
				slotKey = metricTime.Truncate(time.Hour).Unix()
			case 24 * time.Hour:
				// Compute local midnight for this metricTime, then convert to UTC
				tLocal := metricTime.In(loc)
				localMidnight := time.Date(
					tLocal.Year(), tLocal.Month(), tLocal.Day(),
					0, 0, 0, 0,
					loc,
				)
				slotKey = localMidnight.UTC().Unix()
			}

			idx, ok := slotMap[slotKey]
			if !ok {
				continue // outside our requested slots
			}

			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			m, err := decodeMetric(val)
			if err != nil {
				return err
			}
			// Aggregate counters into the result
			for k, v := range m.Counters {
				results[idx].Counters[k] += v.Load()
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return results, nil
}

// GetLastMinutesMetrics returns metrics per minute for last n minutes in requested timezone.
func (mm *MinuteMetricManager) GetLastMinutesMetrics(n int, loc *time.Location) ([]MetricResponse, error) {
	return mm.getAggregatedMetrics(n, time.Minute, loc)
}

// GetLastHoursMetrics returns metrics per hour for last n hours in the requested timezone.
func (mm *MinuteMetricManager) GetLastHoursMetrics(n int, loc *time.Location) ([]MetricResponse, error) {
	return mm.getAggregatedMetrics(n, time.Hour, loc)
}

// GetLastDaysMetrics returns metrics per day for last n days in the requested timezone.
func (mm *MinuteMetricManager) GetLastDaysMetrics(n int, loc *time.Location) ([]MetricResponse, error) {
	return mm.getAggregatedMetrics(n, 24*time.Hour, loc)
}

// getTimeSeries returns the historical values of a specific key for n slots in the requested timezone.
func (mm *MinuteMetricManager) getTimeSeries(key string, n int, slot time.Duration, loc *time.Location) (TimeSeriesResponse, error) {
	metrics, err := mm.getAggregatedMetrics(n, slot, loc)
	if err != nil {
		return nil, err
	}
	result := make(TimeSeriesResponse, n)
	for i, m := range metrics {
		result[i] = TimeSeriesPoint{
			Time:  m.Date, // Already in the requested timezone
			Value: m.Counters[key],
		}
	}
	return result, nil
}

func (mm *MinuteMetricManager) GetMetricValuesLastMinutes(key string, n int, loc *time.Location) (TimeSeriesResponse, error) {
	return mm.getTimeSeries(key, n, time.Minute, loc)
}

func (mm *MinuteMetricManager) GetMetricValuesLastHours(key string, n int, loc *time.Location) (TimeSeriesResponse, error) {
	return mm.getTimeSeries(key, n, time.Hour, loc)
}

func (mm *MinuteMetricManager) GetMetricValuesLastDays(key string, n int, loc *time.Location) (TimeSeriesResponse, error) {
	return mm.getTimeSeries(key, n, 24*time.Hour, loc)
}

const (
	MaxCounterValue uint64 = math.MaxUint64
	Threshold              = MaxCounterValue - 1000000 // Threshold to detect probable overflow
)

// CalculateCounterIncrease is a helper function to compute the increase in a counter-value, handling potential overflows.
func (mm *MinuteMetricManager) CalculateCounterIncrease(previous, current uint64) uint64 {
	var delta uint64
	if current >= previous {
		delta = current - previous
	} else if previous >= Threshold {
		// Probable overflow del contador
		delta = (MaxCounterValue - previous) + current + 1
	} else {
		// Probable reinicio de software
		delta = current
	}

	return delta
}
