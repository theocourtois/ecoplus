package fleet

import "time"

const BillingPeriod = time.Hour

func BillingBoundary(created, now time.Time) time.Time {
	elapsed := now.Sub(created)
	if elapsed < 0 {
		elapsed = 0
	}
	periods := (elapsed + BillingPeriod - 1) / BillingPeriod
	if periods < 1 {
		periods = 1
	}
	return created.Add(periods * BillingPeriod)
}

func Expiry(created, now time.Time, margin time.Duration) time.Time {
	return BillingBoundary(created, now).Add(-margin)
}
