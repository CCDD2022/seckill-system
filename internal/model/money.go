package model

import "math"

const maxProductPrice = 99_999_999.99 // MySQL decimal(10,2)

// ValidProductPrice rejects sub-cent amounts and values that cannot fit the
// products.price column. Float64 remains in the public API for compatibility;
// a payment integration should use integer cents or decimal throughout.
func ValidProductPrice(price float64) bool {
	if math.IsNaN(price) || math.IsInf(price, 0) || price < 0.01 || price > maxProductPrice {
		return false
	}
	cents := price * 100
	return math.Abs(cents-math.Round(cents)) < 1e-6
}
