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
