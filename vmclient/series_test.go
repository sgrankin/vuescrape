package vmclient

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type fields struct {
	Metric  Metric
	Samples []Sample
}

var tests = []struct {
	name   string
	fields fields
	want   string
}{
	{"empty", fields{
		Metric: Metric{Name: "METRIC"}},
		`{"metric":{"__name__":"METRIC"}}`},
	{"points", fields{
		Metric: Metric{Name: "METRIC"},
		Samples: []Sample{
			{Value: 420, Timestamp: time.UnixMilli(42001)},
			{Value: 430, Timestamp: time.UnixMilli(43001)}}},
		`{"metric":{"__name__":"METRIC"},"values":[420,430],"timestamps":[42001,43001]}`},
	{"labels", fields{
		Metric: Metric{Name: "METRIC",
			Labels: map[string]string{"hello": "world", "banana": "phone"}}},
		`{"metric":{"__name__":"METRIC","banana":"phone","hello":"world"}}`},
	{"everything", fields{
		Metric: Metric{Name: "METRIC",
			Labels: map[string]string{"hello": "world", "banana": "phone"}},
		Samples: []Sample{
			{Value: 420, Timestamp: time.UnixMilli(42001)},
			{Value: 430, Timestamp: time.UnixMilli(43001)}}},
		`{"metric":{"__name__":"METRIC","banana":"phone","hello":"world"},"values":[420,430],"timestamps":[42001,43001]}`},
}

func TestSeries_String(t *testing.T) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Series{
				Metric:  tt.fields.Metric,
				Samples: tt.fields.Samples,
			}
			if got := s.String(); got != tt.want {
				t.Errorf("Series.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSeries_MarshalJSON(t *testing.T) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Series{
				Metric:  tt.fields.Metric,
				Samples: tt.fields.Samples,
			}
			got, err := s.MarshalJSON()
			if err != nil {
				t.Fatalf("Series.MarshalJSON() error = %v", err)
			}
			if !reflect.DeepEqual(got, []byte(tt.want)) {
				t.Errorf("Series.MarshalJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSeries_UnmarshalJSON(t *testing.T) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := Series{
				Metric:  tt.fields.Metric,
				Samples: tt.fields.Samples,
			}
			bytes, err := want.MarshalJSON()
			if err != nil {
				t.Fatalf("Series.MarshalJSON() error = %v", err)
			}
			var got Series
			if err := got.UnmarshalJSON(bytes); err != nil {
				t.Fatalf("Series.UnmarshalJSON() error = %v", err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Series.UnmarshalJSON() diff (-want+got):\n%s", diff)
			}
		})
	}
}
