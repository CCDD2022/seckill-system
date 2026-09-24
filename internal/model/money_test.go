package model

import (
	"math"
	"testing"
)

func TestValidProductPriceRejectsSubCentAndOutOfRange(t *testing.T) {
	for _, test := range []struct {
		price float64
		valid bool
	}{
		{0.01, true}, {9.99, true}, {99_999_999.99, true},
		{0, false}, {0.001, false}, {0.005, false},
		{9.999, false}, {100_000_000, false}, {math.NaN(), false},
	} {
		if got := ValidProductPrice(test.price); got != test.valid {
			t.Errorf("ValidProductPrice(%v) = %v, want %v", test.price, got, test.valid)
		}
	}
}
