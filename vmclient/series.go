package vmclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"time"
)

/*
Series represents a single VictoriaMetrics import/export JSON line.

It contains severeal datapoints for a single metric (name+labels combination).
See [docs] for details.

	{
		// metric contans metric name plus labels for a particular time series
		"metric":{
			"__name__": "metric_name",  // <- this is metric name

			// Other labels for the time series
			"label1": "value1",
			"label2": "value2",
			...
			"labelN": "valueN"
		},

	// values contains raw sample values for the given time series
	"values": [1, 2.345, -678],

	// timestamps contains raw sample UNIX timestamps in milliseconds for the given time series
	// every timestamp is associated with the value at the corresponding position
	"timestamps": [1549891472010,1549891487724,1549891503438]
	}

[docs]: https://docs.victoriametrics.com/#json-line-format
*/
type Series struct {
	Metric  Metric
	Samples []Sample
}

type jseries struct {
	Metric     Metric    `json:"metric"`
	Values     []float64 `json:"values,omitempty"`
	Timestamps []int64   `json:"timestamps,omitempty"`
}

// String implements fmt.Stringer.
func (s *Series) String() string {
	buf, _ := s.MarshalJSON()
	return string(buf)
}

// MarshalJSON implements json.Marshaler.
func (s *Series) MarshalJSON() ([]byte, error) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "") // Single line output.

	var doc jseries = jseries{
		Metric:     s.Metric,
		Values:     make([]float64, 0, len(s.Samples)),
		Timestamps: make([]int64, 0, len(s.Samples)),
	}
	for _, s := range s.Samples {
		doc.Values = append(doc.Values, s.Value)
		doc.Timestamps = append(doc.Timestamps, s.Timestamp.UnixMilli())
	}
	enc.Encode(&doc)
	buf.Truncate(buf.Len() - 1) // Encode wrote a '\n' we don't want.
	return buf.Bytes(), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Series) UnmarshalJSON(b []byte) error {
	var doc jseries
	if err := json.Unmarshal(b, &doc); err != nil {
		return err
	}
	s.Metric = doc.Metric
	for i, v := range doc.Values {
		s.Samples = append(s.Samples, Sample{
			Value:     v,
			Timestamp: time.UnixMilli(doc.Timestamps[i]),
		})
	}
	return nil
}

var (
	_ json.Marshaler   = (*Series)(nil)
	_ json.Unmarshaler = (*Series)(nil)
	_ fmt.Stringer     = (*Series)(nil)
)

type Metric struct {
	Name   string
	Labels map[string]string
}

// MarshalJSON implements json.Marshaler.
func (m *Metric) MarshalJSON() ([]byte, error) {
	val := map[string]string{"__name__": m.Name}
	maps.Copy(val, m.Labels)
	return json.MarshalIndent(val, "", "")
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *Metric) UnmarshalJSON(data []byte) error {
	labels := map[string]string{}
	if err := json.Unmarshal(data, &labels); err != nil {
		return err
	}
	m.Name = labels["__name__"]
	delete(labels, "__name__")
	if len(labels) > 0 {
		m.Labels = labels
	} else {
		m.Labels = nil
	}
	return nil
}

var (
	_ json.Marshaler   = (*Metric)(nil)
	_ json.Unmarshaler = (*Metric)(nil)
)

type Sample struct {
	Value     float64
	Timestamp time.Time
}

// MarshalJSON implements json.Marshaler.
func (s *Sample) MarshalJSON() ([]byte, error) {
	return json.MarshalIndent([]any{s.Timestamp.UnixMilli(), fmt.Sprint(s.Value)}, "", "")
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Sample) UnmarshalJSON(data []byte) error {
	var doc []any
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	if len(doc) != 2 {
		return fmt.Errorf("sample: expected array of len=2, got %v", doc)
	}
	ts, ok := doc[0].(float64)
	if !ok {
		return fmt.Errorf("sample timestamp was not an number: %v", doc)
	}
	s.Timestamp = time.Unix(int64(ts), 0)
	val, ok := doc[1].(string)
	if !ok {
		return fmt.Errorf("sample value was not a string: %v", doc)
	}
	var err error
	s.Value, err = strconv.ParseFloat(val, 64)
	if err != nil {
		return fmt.Errorf("sample value was not a float: %v", doc)
	}
	return nil
}

var (
	_ json.Marshaler   = (*Sample)(nil)
	_ json.Unmarshaler = (*Sample)(nil)
)
