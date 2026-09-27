package usage

import (
	"sort"
	"strings"
)

// Price is what a model charges, in US dollars per million tokens.
//
// Cache writes are priced from the input: 1.25 times it for the five-minute
// cache and twice it for the hour, which is how Anthropic bills and so is
// derived rather than written out per model. CacheRead is written out,
// because it stopped being a fixed fraction of the input with the newest
// models.
type Price struct {
	Input     float64 `toml:"input" json:"input"`
	Output    float64 `toml:"output" json:"output"`
	CacheRead float64 `toml:"cache_read" json:"cache_read"`
}

// Prices are the models tend knows the price of, by the start of their id.
// The list is Anthropic's public API prices (September 2026). What is paid
// through a subscription, Bedrock or Vertex is not this, which is why the
// report calls the number an estimate and why [activity.prices] can replace
// any of it.
var Prices = map[string]Price{
	"claude-fable-5-1":  {Input: 10, Output: 50, CacheRead: 0.25},
	"claude-mythos-5-1": {Input: 10, Output: 50, CacheRead: 0.25},
	"claude-fable-5":    {Input: 10, Output: 50, CacheRead: 1},
	"claude-mythos-5":   {Input: 10, Output: 50, CacheRead: 1},
	"claude-opus-5-5":   {Input: 4, Output: 20, CacheRead: 0.20},
	"claude-opus-5":     {Input: 5, Output: 25, CacheRead: 0.50},
	"claude-opus-4-8":   {Input: 5, Output: 25, CacheRead: 0.50},
	"claude-opus-4-7":   {Input: 5, Output: 25, CacheRead: 0.50},
	"claude-opus-4-6":   {Input: 5, Output: 25, CacheRead: 0.50},
	"claude-opus-4-5":   {Input: 5, Output: 25, CacheRead: 0.50},
	"claude-sonnet-5":   {Input: 2, Output: 10, CacheRead: 0.20},
	"claude-sonnet-4-6": {Input: 3, Output: 15, CacheRead: 0.30},
	"claude-sonnet-4-5": {Input: 3, Output: 15, CacheRead: 0.30},
	"claude-haiku-4-5":  {Input: 1, Output: 5, CacheRead: 0.10},
}

// PriceOf finds a model's price: the overrides first, then the table, by the
// longest id the model's starts with. Claude Code writes ids with a date or a
// context suffix on some models ("claude-opus-4-5-20251101"), and the longest
// match is what keeps claude-opus-5-5 from being priced as claude-opus-5.
func PriceOf(model string, overrides map[string]Price) (Price, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	if p, ok := longest(model, overrides); ok {
		return p, true
	}
	return longest(model, Prices)
}

func longest(model string, table map[string]Price) (Price, bool) {
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		lk := strings.ToLower(k)
		if model == lk || strings.HasPrefix(model, lk+"-") || strings.HasPrefix(model, lk+"[") || strings.HasPrefix(model, lk+"@") {
			return table[k], true
		}
	}
	return Price{}, false
}

// Cost is what tokens of one model cost, in dollars.
func (p Price) Cost(t Tokens) float64 {
	write5m := t.CacheWrite - t.CacheWrite1h
	if write5m < 0 {
		write5m = 0
	}
	perM := func(n int64, price float64) float64 { return float64(n) / 1e6 * price }
	return perM(t.Input, p.Input) +
		perM(t.Output, p.Output) +
		perM(write5m, p.Input*1.25) +
		perM(t.CacheWrite1h, p.Input*2) +
		perM(t.CacheRead, p.CacheRead)
}

// Cost is what a usage cost, and whether every model in it had a price. A
// model with none adds nothing and makes the answer a lower bound, which the
// report says rather than presenting it as the whole.
func (u Usage) Cost(overrides map[string]Price) (usd float64, complete bool) {
	complete = true
	for m, t := range u {
		p, ok := PriceOf(m, overrides)
		if !ok {
			complete = false
			continue
		}
		usd += p.Cost(t)
	}
	return usd, complete
}
