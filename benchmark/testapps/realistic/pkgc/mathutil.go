// Package pkgc provides small numeric utilities for the realistic benchmark
// scenario's synthetic "service" endpoints.
package pkgc

import "math"

// Primes returns all primes <= n using a sieve of Eratosthenes.
func Primes(n int) []int {
	if n < 2 {
		return nil
	}
	sieve := make([]bool, n+1)
	var primes []int
	for i := 2; i <= n; i++ {
		if sieve[i] {
			continue
		}
		primes = append(primes, i)
		for j := i * i; j <= n; j += i {
			sieve[j] = true
		}
	}
	return primes
}

// Stats holds basic descriptive statistics.
type Stats struct {
	Mean   float64
	StdDev float64
	Min    float64
	Max    float64
}

// Summarize computes Stats over vals. Returns the zero value for an empty
// slice.
func Summarize(vals []float64) Stats {
	if len(vals) == 0 {
		return Stats{}
	}
	sum := 0.0
	min, max := vals[0], vals[0]
	for _, v := range vals {
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	mean := sum / float64(len(vals))

	variance := 0.0
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	variance /= float64(len(vals))

	return Stats{
		Mean:   mean,
		StdDev: math.Sqrt(variance),
		Min:    min,
		Max:    max,
	}
}

// Fibonacci returns the first n Fibonacci numbers.
func Fibonacci(n int) []int {
	if n <= 0 {
		return nil
	}
	out := make([]int, n)
	for i := range out {
		switch i {
		case 0:
			out[i] = 0
		case 1:
			out[i] = 1
		default:
			out[i] = out[i-1] + out[i-2]
		}
	}
	return out
}
