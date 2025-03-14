# go-metrics

**go-metrics** is a simple and lightweight Go library for tracking, storing, and querying metrics over time. With its intuitive API and timezone-aware queries, it is ideal for applications that need reliable and efficient metric management.

---

## Features

- **Lightweight and Simple**: Designed for ease of use and quick integration into your projects.
- **Atomic Operations**: Safely increment, decrement, or adjust metric values, even in concurrent environments.
- **Time-Based Grouping**: Metrics are grouped by hour or day for granular tracking and analysis.
- **Persistence**: Metrics are automatically saved to a JSON file and reloaded at startup.
- **Timezone-Aware Queries**: Retrieve metrics for specific time ranges, respecting your preferred timezone.
- **Dynamic Metric Creation**: Automatically creates metrics when they are first used.
- **Custom Signal Handling**: The program using the library manages signals and decides when to call `SaveMetrics`.

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
	mm, err := metrics.NewMetricsManager("metrics.json", 30, 5*time.Minute)
	if err != nil {
		fmt.Printf("Error initializing MetricsManager: %v\n", err)
		return
	}

	// Use it to track your metrics!
}
```

- `"metrics.json"`: File path for saving metrics.
- `30`: Number of days to retain metrics.
- `5 * time.Minute`: Interval for automatically saving metrics to disk.

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

### 4. **Handle Signals in Your Program**

The program is responsible for handling OS signals and calling `SaveMetrics` when needed.

```go
import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pablolagos/go-metrics"
)

func main() {
	// Initialize Metrics Manager
	mm, err := metrics.NewMetricsManager("metrics.json", 30, 5*time.Minute)
	if err != nil {
		fmt.Printf("Error initializing MetricsManager: %v\n", err)
		return
	}

	// Simulate some metric activity
	go func() {
		for i := 0; i < 10; i++ {
			mm.Increment("requests")
			time.Sleep(time.Second)
		}
	}()

	// Handle OS signals at the end of the program
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-signalChan
	fmt.Printf("Received signal: %s. Saving metrics...\n", sig)

	// Save metrics before exiting
	if err := mm.SaveMetrics(); err != nil {
		fmt.Printf("Error saving metrics: %v\n", err)
	} else {
		fmt.Println("Metrics saved successfully.")
	}

	fmt.Println("Exiting.")
	os.Exit(0)
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

---

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.

---

## Contributing

Contributions are welcome! Feel free to open issues or submit pull requests to enhance the functionality of **go-metrics**.
