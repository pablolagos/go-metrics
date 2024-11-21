# go-metrics

**go-metrics** is a simple and lightweight Go library for managing, storing, and querying metrics. Designed for ease of use, it provides atomic operations, customizable persistence, and advanced querying capabilities that work seamlessly across different time zones.

---

## Features

- **Lightweight and Simple**: Focused on providing an intuitive interface to track and query metrics.
- **Atomic Operations**: Safely increment, decrement, or adjust metrics, even in concurrent environments.
- **Time-Based Grouping**: Metrics are grouped by hour or day for easy reporting and analysis.
- **Persistence**: Automatically saves metrics to disk in JSON format, ensuring durability.
- **Timezone-Aware Queries**: Retrieve metrics for the last N hours or days, respecting your preferred timezone.
- **Dynamic Metric Creation**: Metrics are created on-the-fly, no need for pre-definition.
- **Signal Handling**: Ensures metrics are saved before termination when receiving system signals like `SIGINT` or `SIGTERM`.

---

## Installation

```sh
go get github.com/pablolagos/go-metrics
```

---

## Usage

### 1. **Initialize the Metrics Manager**

```go
import (
	"github.com/pablolagos/go-metrics"
	"time"
)

func main() {
	// Initialize the Metrics Manager
	mm := metrics.NewMetricsManager("metrics.json", 30, 10 * time.Minute)

	// Use it to track your metrics!
}
```

- `"metrics.json"`: File path for saving metrics.
- `30`: Number of days to retain metrics.
- `10 * time.Minute`: Interval for automatically saving metrics to disk.

---

### 2. **Track Metrics**

#### Increment or Decrement Metrics

```go
// Increment by 1
mm.Increment("requests")

// Decrement by 1
mm.Decrement("errors")
```

#### Adjust Metrics Dynamically

```go
// Increment by a custom value
mm.IncrementBy("processed_files", 10)

// Decrement by a custom value
mm.DecrementBy("failed_jobs", 5)
```

---

### 3. **Query Metrics**

#### Retrieve All Metrics for the Last N Days

```go
import "time"

// Retrieve metrics for the last 7 days in the "Europe/Berlin" timezone
location, _ := time.LoadLocation("Europe/Berlin")
metrics, err := mm.GetMetricsForLastDays(7, location)
if err != nil {
    fmt.Println("Error:", err)
    return
}

for _, metric := range metrics {
    fmt.Printf("Date: %s, Counters: %+v\n", metric.Date.Format("2006-01-02"), metric.Counters)
}
```

#### Retrieve Metrics for the Last N Hours

```go
// Retrieve metrics for the last 12 hours in the "America/New_York" timezone
location, _ := time.LoadLocation("America/New_York")
metrics, err := mm.GetMetricsForLastHours(12, location)
if err != nil {
    fmt.Println("Error:", err)
    return
}

for _, metric := range metrics {
    fmt.Printf("Hour: %s, Counters: %+v\n", metric.Date.Format("2006-01-02 15:00"), metric.Counters)
}
```

#### Query a Specific Metric for the Last N Days

```go
// Retrieve values for a specific metric ("requests") over the last 5 days
values, err := mm.GetMetricValuesForLastDays("requests", 5, location)
if err != nil {
    fmt.Println("Error:", err)
    return
}

for _, value := range values {
    fmt.Printf("Date: %s, Value: %d\n", value.Date.Format("2006-01-02"), value.Value)
}
```

#### Query a Specific Metric for the Last N Hours

```go
// Retrieve values for a specific metric ("errors") over the last 6 hours
values, err := mm.GetMetricValuesForLastHours("errors", 6, location)
if err != nil {
    fmt.Println("Error:", err)
    return
}

for _, value := range values {
    fmt.Printf("Hour: %s, Value: %d\n", value.Date.Format("2006-01-02 15:00"), value.Value)
}
```

---

## JSON Structure

Metrics are saved in JSON format, with timestamps as keys and counters grouped within.

Example:

```json
{
    "2024-11-20T14:00:00Z": {
        "date": "2024-11-20T14:00:00Z",
        "counters": {
            "requests": 100,
            "errors": 2
        }
    },
    "2024-11-20T15:00:00Z": {
        "date": "2024-11-20T15:00:00Z",
        "counters": {
            "requests": 150,
            "errors": 3
        }
    }
}
```

---

## Advanced Features

1. **Atomic Operations**:
    - Increment and decrement values safely in concurrent environments using atomic operations.

2. **Timezone-Aware Queries**:
    - Queries like `GetMetricsForLastDays` and `GetMetricsForLastHours` allow metrics to be retrieved with respect to specific time zones.

3. **Dynamic Metric Creation**:
    - Metrics are created dynamically when `Increment`, `Decrement`, or related methods are called.

4. **Automatic Cleanup**:
    - Removes old metrics beyond the configured `maxDays`.

5. **Signal Handling**:
    - Ensures metrics are saved before program termination.

---

## Example Program

```go
package main

import (
	"fmt"
	"time"

	"github.com/pablolagos/go-metrics"
)

func main() {
	// Initialize Metrics Manager with 30-days of retention and save interval of 5 minutes
	mm := metrics.NewMetricsManager("metrics.json", 30, 5 * time.Minute)

	// Track metrics
	mm.Increment("requests")
	mm.IncrementBy("requests", 5)
	mm.Decrement("errors")
	mm.DecrementBy("errors", 2)

	// Retrieve metrics for the last 2 days
	location, _ := time.LoadLocation("UTC")
	metrics, _ := mm.GetMetricsForLastDays(2, location)
	fmt.Println("Metrics for the last 2 days:")
	for _, metric := range metrics {
		fmt.Printf("Date: %s, Counters: %+v\n", metric.Date.Format("2006-01-02"), metric.Counters)
	}

	// Retrieve specific metric values for the last 24 hours
	values, _ := mm.GetMetricValuesForLastHours("requests", 24, location)
	fmt.Println("\nRequests for the last 24 hours:")
	for _, value := range values {
		fmt.Printf("Hour: %s, Value: %d\n", value.Date.Format("2006-01-02 15:00"), value.Value)
	}
}
```

---

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.

---

## Contributing

Contributions are welcome! Feel free to open issues or submit pull requests to enhance the functionality of **go-metrics**.


