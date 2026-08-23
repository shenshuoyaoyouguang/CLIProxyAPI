package credentialweight

import (
	"encoding/json"
	"math"
	"testing"
)

func TestParseValueValidation(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		want    int64
		wantErr bool
	}{
		{name: "default string", value: "", want: Default},
		{name: "negative excluded", value: json.Number("-5"), want: 0},
		{name: "fraction rejected", value: json.Number("1.5"), wantErr: true},
		{name: "maximum", value: json.Number("1000000"), want: Max},
		{name: "above maximum", value: json.Number("1000001"), wantErr: true},
		{name: "int64 overflow", value: json.Number("9223372036854775808"), wantErr: true},
		// int and the narrower integer kinds route through the same path.
		{name: "int zero", value: int(0), want: 0},
		{name: "int positive", value: int(5), want: 5},
		{name: "int negative", value: int(-5), want: 0},
		{name: "int above maximum", value: int(Max + 1), wantErr: true},
		{name: "int8 positive", value: int8(5), want: 5},
		{name: "int8 negative", value: int8(-5), want: 0},
		{name: "int16 positive", value: int16(5), want: 5},
		{name: "int32 positive", value: int32(5), want: 5},
		{name: "int64 positive", value: int64(5), want: 5},
		{name: "int64 above maximum", value: int64(Max + 1), wantErr: true},
		{name: "uint zero", value: uint(0), want: 0},
		{name: "uint positive", value: uint(5), want: 5},
		{name: "uint above maximum", value: uint(Max + 1), wantErr: true},
		{name: "uint8 positive", value: uint8(255), want: 255},
		{name: "uint16 positive", value: uint16(5), want: 5},
		{name: "uint32 above maximum", value: uint32(Max + 1), wantErr: true},
		{name: "uint64 above maximum", value: uint64(1) << 62, wantErr: true},
		{name: "float32 positive", value: float32(5), want: 5},
		{name: "float32 fraction rejected", value: float32(1.5), wantErr: true},
		{name: "float64 positive", value: float64(5), want: 5},
		{name: "float64 zero", value: float64(0), want: 0},
		{name: "float64 negative", value: float64(-5), want: 0},
		{name: "float64 fraction rejected", value: float64(1.5), wantErr: true},
		{name: "float64 nan rejected", value: math.NaN(), wantErr: true},
		{name: "float64 inf rejected", value: math.Inf(1), wantErr: true},
		{name: "float64 above maximum", value: float64(Max + 1), wantErr: true},
		// Unsupported kinds are rejected uniformly.
		{name: "bool rejected", value: true, wantErr: true},
		{name: "nil rejected", value: nil, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, errParse := ParseValue(test.value)
			if (errParse != nil) != test.wantErr {
				t.Fatalf("ParseValue(%v) error = %v, wantErr=%v", test.value, errParse, test.wantErr)
			}
			if !test.wantErr && got != test.want {
				t.Fatalf("ParseValue(%v) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}
