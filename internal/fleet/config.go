package fleet

import "time"

type Config struct {
	Margin     time.Duration
	ClaimGuard time.Duration
	WipeGuard  time.Duration
}

func DefaultConfig() Config {
	return Config{Margin: 10 * time.Minute, ClaimGuard: 2 * time.Minute, WipeGuard: 5 * time.Minute}
}
