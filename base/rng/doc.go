// Package rng provides a feedable CSPRNG.
//
// CSPRNG used is fortuna: github.com/seehuhn/fortuna
// By default the CSPRNG is fed by two sources:
// - It starts with a seed from `crypto/rand` and periodically reseeds from there
// - A really simple tickfeeder which extracts entropy from the internal go scheduler using goroutines and is meant to be used under load.
//
// The RNG can also be easily fed with additional sources.
package rng
