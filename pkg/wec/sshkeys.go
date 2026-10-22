package wec

import (
	"context"
	"net/http"
)

// SSHKey is a registered SSH public key.
type SSHKey struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
}

// ListSSHKeys returns the account's SSH keys.
func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	return listAll[SSHKey](ctx, c, "/ssh-keys", ListOptions{})
}

// AddSSHKey registers a public key under name.
func (c *Client) AddSSHKey(ctx context.Context, name, publicKey string) (*SSHKey, error) {
	var k SSHKey
	body := map[string]string{"name": name, "public_key": publicKey}
	return &k, c.Do(ctx, http.MethodPost, "/ssh-keys", body, &k, nil)
}

// DeleteSSHKey removes the key called name.
func (c *Client) DeleteSSHKey(ctx context.Context, name string) error {
	return c.Do(ctx, http.MethodDelete, "/ssh-keys/"+PathEscape(name), nil, nil, nil)
}
