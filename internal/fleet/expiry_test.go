package fleet

import (
	"testing"
	"time"
)

func TestExpiry(t *testing.T) {
	created := time.Unix(1_000_000, 0)
	margin := 10 * time.Minute
	cases := map[string]struct {
		elapsed time.Duration
		want    time.Duration
	}{
		"at creation":       {0, 50 * time.Minute},
		"one second":        {time.Second, 50 * time.Minute},
		"just before hour":  {59*time.Minute + 59*time.Second, 50 * time.Minute},
		"exactly on hour":   {time.Hour, 50 * time.Minute},
		"one second past":   {time.Hour + time.Second, 110 * time.Minute},
		"exactly two hours": {2 * time.Hour, 110 * time.Minute},
		"before creation":   {-time.Minute, 50 * time.Minute},
		"deep into the day": {23*time.Hour + time.Minute, 24*time.Hour - margin},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := Expiry(created, created.Add(tc.elapsed), margin).Sub(created)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestExpiryOnBoundaryIsInThePast(t *testing.T) {
	created := time.Unix(0, 0)
	now := created.Add(time.Hour)
	if !Expiry(created, now, 10*time.Minute).Before(now) {
		t.Fatal("expiry must already be past on the boundary")
	}
}

func TestPoolKey(t *testing.T) {
	base := PoolSpec{ServerType: "cx22", Location: "nbg1", SSHKeyIDs: []int64{3, 1, 2}, UserData: "x"}
	reordered := base
	reordered.SSHKeyIDs = []int64{2, 3, 1, 1, 3}
	if PoolKey(base) != PoolKey(reordered) {
		t.Fatal("order and duplicates must not change the key")
	}
	if len(PoolKey(base)) != 16 {
		t.Fatal("key must be 16 hex chars")
	}
	variants := map[string]func(*PoolSpec){
		"type":     func(s *PoolSpec) { s.ServerType = "cx32" },
		"location": func(s *PoolSpec) { s.Location = "fsn1" },
		"keys":     func(s *PoolSpec) { s.SSHKeyIDs = []int64{1} },
		"userdata": func(s *PoolSpec) { s.UserData = "y" },
	}
	for name, mutate := range variants {
		v := base
		mutate(&v)
		if PoolKey(v) == PoolKey(base) {
			t.Errorf("%s must change the key", name)
		}
	}
	empty, nilKeys := PoolSpec{UserData: ""}, PoolSpec{SSHKeyIDs: []int64{}}
	if PoolKey(empty) != PoolKey(nilKeys) {
		t.Fatal("nil and empty key lists must match")
	}
}

func TestRequestHash(t *testing.T) {
	pool := PoolKey(PoolSpec{ServerType: "cx22", Location: "nbg1"})
	base := RequestHash(pool, "ubuntu", "a")
	if base == RequestHash(pool, "debian", "a") || base == RequestHash(pool, "ubuntu", "b") ||
		base == RequestHash(PoolKey(PoolSpec{ServerType: "cx32"}), "ubuntu", "a") {
		t.Fatal("pool, image and name must change the hash")
	}
	if base == pool || len(base) != 16 {
		t.Fatal("request hash must be a distinct 16 char value")
	}
}
