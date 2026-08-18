package service

import "testing"

func TestApplyLowestRateSchedulingPreference(t *testing.T) {
	rateHigh, rateLow := 1.0, 0.5
	accounts := []Account{
		{ID: 1, Priority: 1, RateMultiplier: &rateHigh},
		{ID: 2, Priority: 10, RateMultiplier: &rateLow},
		{ID: 3, Priority: 2, RateMultiplier: &rateLow},
	}

	ApplyLowestRateSchedulingPreference(accounts)
	if accounts[0].ID != 3 || accounts[1].ID != 2 || accounts[2].ID != 1 {
		t.Fatalf("unexpected order: %#v", accounts)
	}
}

func TestApplyLowestRateSchedulingPreferenceUsesDefaultRate(t *testing.T) {
	rate := 0.25
	accounts := []Account{{ID: 1, Priority: 5}, {ID: 2, Priority: 1, RateMultiplier: &rate}}
	ApplyLowestRateSchedulingPreference(accounts)
	if accounts[0].ID != 2 || accounts[1].ID != 1 {
		t.Fatalf("unexpected order: %#v", accounts)
	}
}

func TestCompareAccountsBySchedulingPreferenceRateIsPrimary(t *testing.T) {
	cheapRate, expensiveRate := 0.25, 2.0
	cheap := &Account{ID: 1, Priority: 100, RateMultiplier: &cheapRate}
	expensive := &Account{ID: 2, Priority: 1, RateMultiplier: &expensiveRate}

	if !accountPreferredBySchedulingPreference(cheap, expensive, true, false) {
		t.Fatal("lowest billing multiplier must win over account priority")
	}
	if accountPreferredBySchedulingPreference(cheap, expensive, false, false) {
		t.Fatal("account priority must remain the primary key when preference is disabled")
	}
}

func TestSortAccountsBySchedulingPreferenceKeepsModelFilteredCandidatesInRateOrder(t *testing.T) {
	rateHigh, rateLow := 1.5, 0.5
	accounts := []*Account{
		{ID: 1, Priority: 1, RateMultiplier: &rateHigh},
		{ID: 2, Priority: 20, RateMultiplier: &rateLow},
		{ID: 3, Priority: 5, RateMultiplier: &rateLow},
	}

	sortAccountsBySchedulingPreference(accounts, true, false)
	if got := []int64{accounts[0].ID, accounts[1].ID, accounts[2].ID}; got[0] != 3 || got[1] != 2 || got[2] != 1 {
		t.Fatalf("unexpected candidate order: %v", got)
	}
}

func TestSortAccountValuesBySchedulingPreferenceReordersValueSlice(t *testing.T) {
	rateHigh, rateLow := 1.5, 0.5
	accounts := []Account{
		{ID: 1, Priority: 1, RateMultiplier: &rateHigh},
		{ID: 2, Priority: 20, RateMultiplier: &rateLow},
		{ID: 3, Priority: 5, RateMultiplier: &rateLow},
	}

	sortAccountValuesBySchedulingPreference(accounts, true, false)
	if got := []int64{accounts[0].ID, accounts[1].ID, accounts[2].ID}; got[0] != 3 || got[1] != 2 || got[2] != 1 {
		t.Fatalf("unexpected value slice order: %v", got)
	}
}
