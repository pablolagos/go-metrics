package go_metrics

import (
	"encoding/json"
	"sync/atomic"
)

// AtomicInt64 is a wrapper around atomic.Int64 to support JSON serialization and deserialization.
type AtomicInt64 struct {
	val atomic.Int64
}

// Add increments or decrements the atomic value by the specified delta.
func (a *AtomicInt64) Add(delta int64) {
	a.val.Add(delta)
}

// Load returns the current value of the atomic integer.
func (a *AtomicInt64) Load() int64 {
	return a.val.Load()
}

// Store sets the value of the atomic integer.
func (a *AtomicInt64) Store(value int64) {
	a.val.Store(value)
}

// MarshalJSON serializes the atomic value into JSON format.
func (a *AtomicInt64) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.val.Load())
}

// UnmarshalJSON deserializes the JSON data into the atomic value.
func (a *AtomicInt64) UnmarshalJSON(data []byte) error {
	var value int64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	a.val.Store(value)
	return nil
}
