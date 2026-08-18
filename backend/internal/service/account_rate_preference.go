package service

import "sort"

// compareAccountsBySchedulingPreference compares two already-filtered account
// candidates. When preferLowestRate is enabled, the account billing multiplier
// is the primary key; the existing priority/LRU tie-breakers remain unchanged.
// A negative result means a should be selected before b.
func compareAccountsBySchedulingPreference(a, b *Account, preferLowestRate, preferOAuth bool) int {
	if a == nil || b == nil {
		switch {
		case a == nil && b == nil:
			return 0
		case a == nil:
			return 1
		default:
			return -1
		}
	}
	if preferLowestRate {
		leftRate, rightRate := a.BillingRateMultiplier(), b.BillingRateMultiplier()
		if leftRate < rightRate {
			return -1
		}
		if leftRate > rightRate {
			return 1
		}
	}
	if a.Priority < b.Priority {
		return -1
	}
	if a.Priority > b.Priority {
		return 1
	}
	switch {
	case a.LastUsedAt == nil && b.LastUsedAt != nil:
		return -1
	case a.LastUsedAt != nil && b.LastUsedAt == nil:
		return 1
	case a.LastUsedAt == nil && b.LastUsedAt == nil:
		if preferOAuth && a.Type != b.Type {
			if a.Type == AccountTypeOAuth {
				return -1
			}
			return 1
		}
		return 0
	default:
		if a.LastUsedAt.Before(*b.LastUsedAt) {
			return -1
		}
		if b.LastUsedAt.Before(*a.LastUsedAt) {
			return 1
		}
		return 0
	}
}

func accountPreferredBySchedulingPreference(candidate, current *Account, preferLowestRate, preferOAuth bool) bool {
	return compareAccountsBySchedulingPreference(candidate, current, preferLowestRate, preferOAuth) < 0
}

func sortAccountsBySchedulingPreference(accounts []*Account, preferLowestRate, preferOAuth bool) {
	sort.SliceStable(accounts, func(i, j int) bool {
		return compareAccountsBySchedulingPreference(accounts[i], accounts[j], preferLowestRate, preferOAuth) < 0
	})
}

func sortAccountValuesBySchedulingPreference(accounts []Account, preferLowestRate, preferOAuth bool) {
	if len(accounts) < 2 {
		return
	}
	sort.SliceStable(accounts, func(i, j int) bool {
		return compareAccountsBySchedulingPreference(&accounts[i], &accounts[j], preferLowestRate, preferOAuth) < 0
	})
}

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
