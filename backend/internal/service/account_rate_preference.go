package service

import "sort"

// ApplyLowestRateSchedulingPreference rewrites only the candidate copies used
// by the scheduler. Persisted account priorities are never changed.
func ApplyLowestRateSchedulingPreference(accounts []Account) {
	if len(accounts) < 2 {
		return
	}

	sort.SliceStable(accounts, func(i, j int) bool {
		leftRate := accounts[i].BillingRateMultiplier()
		rightRate := accounts[j].BillingRateMultiplier()
		if leftRate != rightRate {
			return leftRate < rightRate
		}
		return accounts[i].Priority < accounts[j].Priority
	})

	// Give each (rate, original priority) bucket a synthetic priority. The
	// existing load, sticky-session, and availability logic remains unchanged.
	rank := 0
	previousRate := accounts[0].BillingRateMultiplier()
	previousPriority := accounts[0].Priority
	accounts[0].Priority = rank
	for i := 1; i < len(accounts); i++ {
		rate := accounts[i].BillingRateMultiplier()
		priority := accounts[i].Priority
		if rate != previousRate || priority != previousPriority {
			rank++
			previousRate = rate
			previousPriority = priority
		}
		accounts[i].Priority = rank
	}
}
