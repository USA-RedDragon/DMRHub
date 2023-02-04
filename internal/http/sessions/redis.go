package sessions

// This is a modified version of <https://github.com/gin-contrib/sessions>
// to use a redis client instead of creating its own
// See their license: <https://github.com/gin-contrib/sessions/blob/828f9855fb30f12d40251e5211df3be5c7973e8c/LICENSE>

import (
	"errors"

	"github.com/gin-contrib/sessions"
	"github.com/redis/go-redis/v9"
)

type Store interface {
	sessions.Store
}

// Keys are defined in pairs to allow key rotation, but the common case is to set a single
// authentication key and optionally an encryption key.
//
// The first key in a pair is used for authentication and the second for encryption. The
// encryption key can be set to nil or omitted in the last pair, but the authentication key
// is required in all pairs.
//
// It is recommended to use an authentication key with 32 or 64 bytes. The encryption key,
// if set, must be either 16, 24, or 32 bytes to select AES-128, AES-192, or AES-256 modes.
func NewStore(db *redis.Client, keyPairs ...[]byte) (Store, error) {
	s, err := NewRediStore(db, keyPairs...)
	if err != nil {
		return nil, err
	}
	return &store{s}, nil
}

type store struct {
	*RediStore
}

// GetRedisStore get the actual woking store.
// Ref: https://godoc.org/github.com/boj/redistore#RediStore
func GetRedisStore(s Store) (rediStore *RediStore, err error) {
	realStore, ok := s.(*store)
	if !ok {
		err = errors.New("unable to get the redis store: Store isn't *store")
		return
	}

	rediStore = realStore.RediStore
	return
}

// SetKeyPrefix sets the key prefix in the redis database.
func SetKeyPrefix(s Store, prefix string) error {
	rediStore, err := GetRedisStore(s)
	if err != nil {
		return err
	}

	rediStore.SetKeyPrefix(prefix)
	return nil
}

func (c *store) Options(options sessions.Options) {
	c.RediStore.Options = options.ToGorillaOptions()
}
