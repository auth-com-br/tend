package ui

import (
	"strconv"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
)

// UsageText is an agent's spending as the sidebar's tokens say it (issue
// #46), and how near each gauge is to running out.
type UsageText struct {
	Context, Spent, Cost, Limit string
	ContextLevel, LimitLevel    uint8
}

// Thresholds at which a gauge turns: near the end, and at it. The same the
// server gives its notices at.
const (
	gaugeNear = 80
	gaugeOut  = 95
)

// FormatUsage says u in a few characters each: "ctx 42%", "1.2M tok",
// "$3.10" ("≥$3.10" when a model in it has no price), "limit 61%". now is
// for when a reached limit resets. A nil u says nothing.
func FormatUsage(u *proto.PaneUsage, now time.Time) UsageText {
	var out UsageText
	if u == nil {
		return out
	}
	if pct := u.ContextPercent(); pct >= 0 {
		out.Context = "ctx " + strconv.Itoa(pct) + "%"
		out.ContextLevel = level(float64(pct))
	}
	if u.Tokens > 0 {
		out.Spent = Count(u.Tokens) + " tok"
	}
	if u.Cost > 0 {
		out.Cost = "$" + strconv.FormatFloat(u.Cost, 'f', 2, 64)
		if !u.CostKnown {
			out.Cost = "≥" + out.Cost
		}
	}
	if l := u.Limit; l != nil {
		out.Limit = "limit " + strconv.Itoa(int(l.UsedPercent)) + "%"
		out.LimitLevel = level(l.UsedPercent)
		if l.Reached {
			out.Limit, out.LimitLevel = "limit hit", 2
			if l.Resets > 0 {
				out.Limit += " · " + Until(time.Unix(l.Resets, 0), now)
			}
		}
	}
	return out
}

func level(pct float64) uint8 {
	switch {
	case pct >= gaugeOut:
		return 2
	case pct >= gaugeNear:
		return 1
	}
	return 0
}

// Count says a number of tokens the way an agent's own status line does:
// 950, 12k, 1.2M, 3.4G.
func Count(n int64) string {
	switch {
	case n < 1000:
		return strconv.FormatInt(n, 10)
	case n < 100_000:
		return trimZero(float64(n)/1e3) + "k"
	case n < 1_000_000:
		return strconv.FormatInt(n/1000, 10) + "k"
	case n < 1_000_000_000:
		return trimZero(float64(n)/1e6) + "M"
	}
	return trimZero(float64(n)/1e9) + "G"
}

// trimZero is x with one decimal, unless it is a whole number or ten and
// over.
func trimZero(x float64) string {
	if x >= 10 {
		return strconv.Itoa(int(x))
	}
	s := strconv.FormatFloat(x, 'f', 1, 64)
	if len(s) > 2 && s[len(s)-2:] == ".0" {
		s = s[:len(s)-2]
	}
	return s
}

// Until says how long until t: "in 3h 20m", "in 12m", "in 2d".
func Until(t, now time.Time) string {
	d := t.Sub(now)
	switch {
	case d <= 0:
		return "now"
	case d < time.Hour:
		return "in " + strconv.Itoa(int(d.Minutes())+1) + "m"
	case d < 24*time.Hour:
		h := int(d.Hours())
		return "in " + strconv.Itoa(h) + "h " + strconv.Itoa(int(d.Minutes())-60*h) + "m"
	}
	return "in " + strconv.Itoa(int(d.Hours()/24)) + "d"
}
