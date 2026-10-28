package secrets

import "github.com/zalando/go-keyring"

type realKeyring struct{}

func (realKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (realKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
