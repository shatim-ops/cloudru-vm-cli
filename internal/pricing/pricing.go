// Package pricing produces a monthly cost estimate for a VM configuration.
//
// Cloud.ru does not expose a stable per-flavor price endpoint, so this is a
// transparent rate-card ESTIMATE, not a billed price. It can be overridden by a
// JSON file at ~/.cloudru-vm/pricing.json:
//
//	{
//	  "currency": "RUB",
//	  "flavor_hourly": { "lowcost10-2-4": 2.80 },
//	  "vcpu_hourly": 0.60,
//	  "ram_gb_hourly": 0.40,
//	  "disk_ssd_gb_monthly": 7.0,
//	  "disk_hdd_gb_monthly": 3.0
//	}
//
// Any field present in the file overrides the built-in default.
package pricing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// hoursPerMonth is the conventional cloud billing month (730h).
const hoursPerMonth = 730.0

// Estimate is a cost breakdown for one VM. The fields are unambiguous on
// purpose: total_monthly_rub already includes the disk, so compute and disk are
// breakdown components — never add total + disk.
type Estimate struct {
	Currency          string  `json:"currency"`
	HourlyRub         float64 `json:"hourly_rub"`          // compute, per hour
	ComputeMonthlyRub float64 `json:"compute_monthly_rub"` // compute only
	DiskMonthlyRub    float64 `json:"disk_monthly_rub"`    // disk only
	TotalMonthlyRub   float64 `json:"total_monthly_rub"`   // compute + disk
	Source            string  `json:"source"`
}

// card holds the (overridable) rate card.
type card struct {
	Currency         string             `json:"currency"`
	FlavorHourly     map[string]float64 `json:"flavor_hourly"`
	VCPUHourly       float64            `json:"vcpu_hourly"`
	RAMGBHourly      float64            `json:"ram_gb_hourly"`
	DiskSSDGBMonthly float64            `json:"disk_ssd_gb_monthly"`
	DiskHDDGBMonthly float64            `json:"disk_hdd_gb_monthly"`
}

func defaultCard() card {
	return card{
		Currency:         "RUB",
		FlavorHourly:     map[string]float64{},
		VCPUHourly:       0.60,
		RAMGBHourly:      0.40,
		DiskSSDGBMonthly: 7.0,
		DiskHDDGBMonthly: 3.0,
	}
}

// load merges the optional override file onto the default card.
func load() card {
	c := defaultCard()
	home, err := os.UserHomeDir()
	if err != nil {
		return c
	}
	data, err := os.ReadFile(filepath.Join(home, ".cloudru-vm", "pricing.json"))
	if err != nil {
		return c
	}
	var override card
	if err := json.Unmarshal(data, &override); err != nil {
		return c
	}
	if override.Currency != "" {
		c.Currency = override.Currency
	}
	for k, v := range override.FlavorHourly {
		c.FlavorHourly[k] = v
	}
	if override.VCPUHourly > 0 {
		c.VCPUHourly = override.VCPUHourly
	}
	if override.RAMGBHourly > 0 {
		c.RAMGBHourly = override.RAMGBHourly
	}
	if override.DiskSSDGBMonthly > 0 {
		c.DiskSSDGBMonthly = override.DiskSSDGBMonthly
	}
	if override.DiskHDDGBMonthly > 0 {
		c.DiskHDDGBMonthly = override.DiskHDDGBMonthly
	}
	return c
}

// Estimate computes a cost estimate for a flavor + disk. flavorName may be
// empty (then vCPU/RAM rates are used); diskType is matched case-insensitively
// against "ssd"/"hdd".
func EstimateFor(flavorName string, vcpus, ramGB int, diskGB int, diskType string) Estimate {
	c := load()

	var hourly float64
	source := "estimate: rate-card (vCPU+RAM); not a billed price"
	if v, ok := c.FlavorHourly[flavorName]; ok {
		hourly = v
		source = "estimate: rate-card (flavor); not a billed price"
	} else {
		hourly = float64(vcpus)*c.VCPUHourly + float64(ramGB)*c.RAMGBHourly
	}

	perGB := c.DiskSSDGBMonthly
	if strings.Contains(strings.ToLower(diskType), "hdd") {
		perGB = c.DiskHDDGBMonthly
	}
	diskMonthly := float64(diskGB) * perGB

	computeMonthly := hourly * hoursPerMonth
	return Estimate{
		Currency:          c.Currency,
		HourlyRub:         round2(hourly),
		ComputeMonthlyRub: round2(computeMonthly),
		DiskMonthlyRub:    round2(diskMonthly),
		TotalMonthlyRub:   round2(computeMonthly + diskMonthly),
		Source:            source,
	}
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}
