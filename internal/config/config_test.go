package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) Getenv { return func(k string) string { return m[k] } }

func base() map[string]string {
	return map[string]string{"HCLOUD_TOKEN": "h", "ECOPLUS_TOKEN": "t"}
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.AdminAddr != ":9090" || c.SelfAddr != "" || c.Workers != 8 || c.APIRate != 0.6 ||
		c.Fleet.Margin != 10*time.Minute || c.Fleet.ClaimGuard != 2*time.Minute || c.Fleet.WipeGuard != 5*time.Minute ||
		c.Resync != 30*time.Second || c.LowWatermark != 600 || c.Heartbeat != time.Minute || c.LeaseAlert != 24*time.Hour {
		t.Fatalf("unexpected defaults %+v", c)
	}
}

func TestOverrides(t *testing.T) {
	m := base()
	m["ECOPLUS_ADDR"], m["ECOPLUS_MARGIN"], m["ECOPLUS_WORKERS"], m["ECOPLUS_API_RATE"] = ":1", "20m", "3", "1.5"
	m["ECOPLUS_INSTANCE_ID"], m["ECOPLUS_SELF_ADDR"], m["ECOPLUS_API_LOW_WATERMARK"] = "me", ":2", "10"
	c, err := Load(env(m))
	if err != nil || c.Addr != ":1" || c.Fleet.Margin != 20*time.Minute || c.Workers != 3 || c.APIRate != 1.5 ||
		c.InstanceID != "me" || c.SelfAddr != ":2" || c.LowWatermark != 10 {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestValidation(t *testing.T) {
	cases := map[string]struct {
		set  map[string]string
		drop string
		want string
	}{
		"missing hcloud":   {nil, "HCLOUD_TOKEN", "HCLOUD_TOKEN is required"},
		"missing token":    {nil, "ECOPLUS_TOKEN", "ECOPLUS_TOKEN is required"},
		"margin vs claim":  {map[string]string{"ECOPLUS_MARGIN": "2m"}, "", "MARGIN must be greater"},
		"wipe below claim": {map[string]string{"ECOPLUS_WIPE_GUARD": "1m"}, "", "WIPE_GUARD must be at least"},
		"bad duration":     {map[string]string{"ECOPLUS_RESYNC": "soon"}, "", "ECOPLUS_RESYNC"},
		"bad int":          {map[string]string{"ECOPLUS_WORKERS": "x"}, "", "ECOPLUS_WORKERS"},
		"zero workers":     {map[string]string{"ECOPLUS_WORKERS": "0"}, "", "at least 1"},
		"bad float":        {map[string]string{"ECOPLUS_API_RATE": "x"}, "", "ECOPLUS_API_RATE"},
		"zero rate":        {map[string]string{"ECOPLUS_API_RATE": "0"}, "", "must be positive"},
		"zero claim guard": {map[string]string{"ECOPLUS_CLAIM_GUARD": "0s"}, "", "CLAIM_GUARD must be positive"},
		"zero resync":      {map[string]string{"ECOPLUS_RESYNC": "0s"}, "", "durations must be positive"},
		"negative mark":    {map[string]string{"ECOPLUS_API_LOW_WATERMARK": "-1"}, "", "must not be negative"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := base()
			for k, v := range tc.set {
				m[k] = v
			}
			if tc.drop != "" {
				delete(m, tc.drop)
			}
			if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}
