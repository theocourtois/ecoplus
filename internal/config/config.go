package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/theocourtois/ecoplus/internal/fleet"
)

type Config struct {
	HCloudToken  string
	Token        string
	Addr         string
	AdminAddr    string
	SelfAddr     string
	Fleet        fleet.Config
	Resync       time.Duration
	Workers      int
	APIRate      float64
	APIBurst     int
	LowWatermark int
	Heartbeat    time.Duration
	LeaseAlert   time.Duration
	InstanceID   string
}

type Getenv func(string) string

func Defaults() Config {
	hostname, _ := os.Hostname()
	return Config{
		Addr: ":8080", AdminAddr: ":9090", Fleet: fleet.DefaultConfig(), Resync: 30 * time.Second, Workers: 8,
		APIRate: 0.6, APIBurst: 20, LowWatermark: 600, Heartbeat: 60 * time.Second, LeaseAlert: 24 * time.Hour,
		InstanceID: hostname,
	}
}

func Load(getenv Getenv) (Config, error) {
	c := Defaults()
	var errs []error
	c.HCloudToken = required(getenv, "HCLOUD_TOKEN", &errs)
	c.Token = required(getenv, "ECOPLUS_TOKEN", &errs)
	c.Addr = str(getenv, "ECOPLUS_ADDR", c.Addr)
	c.AdminAddr = str(getenv, "ECOPLUS_ADMIN_ADDR", c.AdminAddr)
	c.SelfAddr = str(getenv, "ECOPLUS_SELF_ADDR", c.SelfAddr)
	c.InstanceID = str(getenv, "ECOPLUS_INSTANCE_ID", c.InstanceID)
	for _, d := range []struct {
		key string
		dst *time.Duration
	}{
		{"ECOPLUS_MARGIN", &c.Fleet.Margin}, {"ECOPLUS_CLAIM_GUARD", &c.Fleet.ClaimGuard},
		{"ECOPLUS_WIPE_GUARD", &c.Fleet.WipeGuard}, {"ECOPLUS_RESYNC", &c.Resync},
		{"ECOPLUS_HEARTBEAT", &c.Heartbeat}, {"ECOPLUS_LEASE_ALERT", &c.LeaseAlert},
	} {
		parseDuration(getenv, d.key, d.dst, &errs)
	}
	parseInt(getenv, "ECOPLUS_WORKERS", &c.Workers, &errs)
	parseInt(getenv, "ECOPLUS_API_LOW_WATERMARK", &c.LowWatermark, &errs)
	parseFloat(getenv, "ECOPLUS_API_RATE", &c.APIRate, &errs)
	errs = append(errs, c.validate()...)
	return c, errors.Join(errs...)
}

func (c Config) validate() []error {
	var errs []error
	if c.Fleet.Margin <= c.Fleet.ClaimGuard {
		errs = append(errs, errors.New("ECOPLUS_MARGIN must be greater than ECOPLUS_CLAIM_GUARD"))
	}
	if c.Fleet.WipeGuard < c.Fleet.ClaimGuard {
		errs = append(errs, errors.New("ECOPLUS_WIPE_GUARD must be at least ECOPLUS_CLAIM_GUARD"))
	}
	if c.Fleet.ClaimGuard <= 0 {
		errs = append(errs, errors.New("ECOPLUS_CLAIM_GUARD must be positive"))
	}
	if c.Workers < 1 {
		errs = append(errs, errors.New("ECOPLUS_WORKERS must be at least 1"))
	}
	if c.APIRate <= 0 {
		errs = append(errs, errors.New("ECOPLUS_API_RATE must be positive"))
	}
	if c.Resync <= 0 || c.Heartbeat <= 0 || c.LeaseAlert <= 0 {
		errs = append(errs, errors.New("durations must be positive"))
	}
	if c.LowWatermark < 0 {
		errs = append(errs, errors.New("ECOPLUS_API_LOW_WATERMARK must not be negative"))
	}
	return errs
}

func required(getenv Getenv, key string, errs *[]error) string {
	v := getenv(key)
	if v == "" {
		*errs = append(*errs, fmt.Errorf("%s is required", key))
	}
	return v
}

func str(getenv Getenv, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDuration(getenv Getenv, key string, dst *time.Duration, errs *[]error) {
	v := getenv(key)
	if v == "" {
		return
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %w", key, err))
		return
	}
	*dst = d
}

func parseInt(getenv Getenv, key string, dst *int, errs *[]error) {
	v := getenv(key)
	if v == "" {
		return
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %w", key, err))
		return
	}
	*dst = n
}

func parseFloat(getenv Getenv, key string, dst *float64, errs *[]error) {
	v := getenv(key)
	if v == "" {
		return
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %w", key, err))
		return
	}
	*dst = f
}
