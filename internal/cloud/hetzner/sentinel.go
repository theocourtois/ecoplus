package hetzner

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strconv"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/theocourtois/ecoplus/internal/cloud"
	"golang.org/x/crypto/ssh"
)

const (
	labelWriter    = "ecoplus/writer"
	labelHeartbeat = "ecoplus/heartbeat"
)

func (c *Client) ReadSentinel(ctx context.Context) (cloud.Sentinel, error) {
	key, _, err := c.api.SSHKey.GetByName(ctx, cloud.SentinelName)
	if err != nil {
		return cloud.Sentinel{}, classify("read sentinel", err)
	}
	if key == nil {
		return cloud.Sentinel{}, nil
	}
	beat, _ := strconv.ParseInt(key.Labels[labelHeartbeat], 10, 64)
	return cloud.Sentinel{Writer: key.Labels[labelWriter], Heartbeat: beat}, nil
}

func (c *Client) WriteSentinel(ctx context.Context, s cloud.Sentinel) error {
	labels := map[string]string{labelWriter: s.Writer, labelHeartbeat: strconv.FormatInt(s.Heartbeat, 10)}
	key, _, err := c.api.SSHKey.GetByName(ctx, cloud.SentinelName)
	if err != nil {
		return classify("write sentinel", err)
	}
	if key != nil {
		_, _, err = c.api.SSHKey.Update(ctx, key, hcloud.SSHKeyUpdateOpts{Labels: labels})
		return classify("write sentinel", err)
	}
	public, err := generatePublicKey()
	if err != nil {
		return err
	}
	_, _, err = c.api.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{Name: cloud.SentinelName, PublicKey: public, Labels: labels})
	return classify("create sentinel", err)
}

func generatePublicKey() (string, error) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", err
	}
	return string(ssh.MarshalAuthorizedKey(sshPub)), nil
}
