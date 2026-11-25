package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

type PoolSpec struct {
	ServerType string  `json:"server_type"`
	Location   string  `json:"location"`
	SSHKeyIDs  []int64 `json:"ssh_key_ids"`
	UserData   string  `json:"user_data"`
}

type requestSpec struct {
	Pool  string `json:"pool"`
	Image string `json:"image"`
	Name  string `json:"name"`
}

func (s PoolSpec) canonical() PoolSpec {
	seen := make(map[int64]struct{}, len(s.SSHKeyIDs))
	ids := make([]int64, 0, len(s.SSHKeyIDs))
	for _, id := range s.SSHKeyIDs {
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	s.SSHKeyIDs = ids
	return s
}

func digest(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

func PoolKey(s PoolSpec) string {
	return digest(s.canonical())
}

func RequestHash(pool, image, name string) string {
	return digest(requestSpec{Pool: pool, Image: image, Name: name})
}
