// Copyright 2012 Brian "bojo" Jones. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// This is a modified version of <https://github.com/boj/redistore>
// to use a redis client instead of creating its own
// See their license: <https://github.com/boj/redistore/blob/cd5dcc76aeff9ba06b0a924829fe24fd69cdd517/LICENSE>

package sessions

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
)

// Amount of time for cookies/redis keys to expire.
var sessionExpire = 86400 * 30 //nolint:golint,gochecknoglobals

// SessionSerializer provides an interface hook for alternative serializers.
type SessionSerializer interface {
	Deserialize(d []byte, ss *sessions.Session) error
	Serialize(ss *sessions.Session) ([]byte, error)
}

// JSONSerializer encode the session map to JSON.
type JSONSerializer struct{}

var (
	ErrNonStringKey     = errors.New("non-string key value, cannot serialize session to JSON")
	ErrStoreValueTooBig = errors.New("the value to store is too big")
	ErrMarshal          = errors.New("error marshaling session")
	ErrUnmarshal        = errors.New("error unmarshaling session")
	ErrSerialization    = errors.New("error serializing session")
	ErrDeserialization  = errors.New("error deserializing session")
	ErrClose            = errors.New("error closing session")
	ErrGetSession       = errors.New("error getting session")
	ErrDeletingSession  = errors.New("error deleting session")
	ErrSavingSession    = errors.New("error saving session")
	ErrCookieEncode     = errors.New("error encoding cookie")
	ErrRedis            = errors.New("error with redis")
	ErrSetExpiration    = errors.New("error setting expiration")
)

// Serialize to JSON. Will err if there are unmarshalable key values.
func (s JSONSerializer) Serialize(ss *sessions.Session) ([]byte, error) {
	m := make(map[string]interface{}, len(ss.Values))
	for k, v := range ss.Values {
		ks, ok := k.(string)
		if !ok {
			fmt.Printf("redistore.JSONSerializer.serialize(). Key: %v Error: %v", k, ErrNonStringKey)
			return nil, ErrNonStringKey
		}
		m[ks] = v
	}
	ret, err := json.Marshal(m)
	if err != nil {
		fmt.Printf("redistore.JSONSerializer.serialize() Error: %v", err)
		return nil, ErrMarshal
	}
	return ret, nil
}

// Deserialize back to map[string]interface{}.
func (s JSONSerializer) Deserialize(d []byte, ss *sessions.Session) error {
	m := make(map[string]interface{})
	err := json.Unmarshal(d, &m)
	if err != nil {
		fmt.Printf("redistore.JSONSerializer.deserialize() Error: %v", err)
		return ErrUnmarshal
	}
	for k, v := range m {
		ss.Values[k] = v
	}
	return nil
}

// GobSerializer uses gob package to encode the session map.
type GobSerializer struct{}

// Serialize using gob.
func (s GobSerializer) Serialize(ss *sessions.Session) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(ss.Values)
	if err == nil {
		return buf.Bytes(), nil
	}
	return nil, ErrSerialization
}

// Deserialize back to map[interface{}]interface{}.
func (s GobSerializer) Deserialize(d []byte, ss *sessions.Session) error {
	dec := gob.NewDecoder(bytes.NewBuffer(d))
	err := dec.Decode(&ss.Values)
	if err != nil {
		return ErrDeserialization
	}
	return nil
}

// RediStore stores sessions in a redis backend.
type RediStore struct {
	DB            *redis.Client
	Codecs        []securecookie.Codec
	Options       *sessions.Options // default configuration
	DefaultMaxAge int               // default Redis TTL for a MaxAge == 0 session
	maxLength     int
	keyPrefix     string
	serializer    SessionSerializer
}

// SetMaxLength sets RediStore.maxLength if the `l` argument is greater or equal 0
// maxLength restricts the maximum length of new sessions to l.
// If l is 0 there is no limit to the size of a session, use with caution.
// The default for a new RediStore is 4096. Redis allows for max.
// value sizes of up to 512MB (http://redis.io/topics/data-types)
// Default: 4096.
func (s *RediStore) SetMaxLength(l int) {
	if l >= 0 {
		s.maxLength = l
	}
}

// SetKeyPrefix set the prefix.
func (s *RediStore) SetKeyPrefix(p string) {
	s.keyPrefix = p
}

// SetSerializer sets the serializer.
func (s *RediStore) SetSerializer(ss SessionSerializer) {
	s.serializer = ss
}

// SetMaxAge restricts the maximum age, in seconds, of the session record
// both in database and a browser. This is to change session storage configuration.
// If you want just to remove session use your session `s` object and change it's
// `Options.MaxAge` to -1, as specified in
//
//	http://godoc.org/github.com/gorilla/sessions#Options
//
// Default is the one provided by this package value - `sessionExpire`.
// Set it to 0 for no restriction.
// Because we use `MaxAge` also in SecureCookie crypting algorithm you should
// use this function to change `MaxAge` value.
func (s *RediStore) SetMaxAge(v int) {
	var c *securecookie.SecureCookie
	var ok bool
	s.Options.MaxAge = v
	for i := range s.Codecs {
		if c, ok = s.Codecs[i].(*securecookie.SecureCookie); ok {
			c.MaxAge(v)
		} else {
			fmt.Printf("Can't change MaxAge on codec %v\n", s.Codecs[i])
		}
	}
}

const defaultMaxAge = 20 * 60 // 20 minutes seems like a reasonable default
const maxLength = 4096

// NewRediStore instantiates a RediStore.
func NewRediStore(db *redis.Client, keyPairs ...[]byte) (*RediStore, error) {
	rs := &RediStore{
		DB:     db,
		Codecs: securecookie.CodecsFromPairs(keyPairs...),
		Options: &sessions.Options{
			Path:   "/",
			MaxAge: sessionExpire,
		},
		DefaultMaxAge: defaultMaxAge,
		maxLength:     maxLength,
		keyPrefix:     "session_",
		serializer:    GobSerializer{},
	}
	_, err := rs.ping(context.Background())
	return rs, err
}

// Close closes the underlying *redis.Pool.
func (s *RediStore) Close() error {
	err := s.DB.Close()
	if err != nil {
		return ErrClose
	}
	return nil
}

// Get returns a session for the given name after adding it to the registry.
//
// See gorilla/sessions FilesystemStore.Get().
func (s *RediStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	ret, err := sessions.GetRegistry(r).Get(s, name)
	if err != nil {
		return nil, ErrGetSession
	}
	return ret, nil
}

// New returns a session for the given name without adding it to the registry.
//
// See gorilla/sessions FilesystemStore.New().
func (s *RediStore) New(r *http.Request, name string) (*sessions.Session, error) {
	var (
		err error
		ok  bool
	)
	session := sessions.NewSession(s, name)
	// make a copy
	options := *s.Options
	session.Options = &options
	session.IsNew = true
	if c, errCookie := r.Cookie(name); errCookie == nil {
		err = securecookie.DecodeMulti(name, c.Value, &session.ID, s.Codecs...)
		if err == nil {
			ok, err = s.load(r.Context(), session)
			session.IsNew = !(err == nil && ok) // not new if no error and data available
		}
	}
	return session, err
}

// Save adds a single session to the response.
func (s *RediStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	// Marked for deletion.
	if session.Options.MaxAge <= 0 {
		if err := s.delete(r.Context(), session); err != nil {
			return ErrDeletingSession
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
	} else {
		// Build an alphanumeric key for the redis store.
		if session.ID == "" {
			const keyLength = 32
			session.ID = strings.TrimRight(base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(keyLength)), "=")
		}
		if err := s.save(r.Context(), session); err != nil {
			return ErrSavingSession
		}
		encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, s.Codecs...)
		if err != nil {
			return ErrCookieEncode
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), encoded, session.Options))
	}
	return nil
}

// Delete removes the session from redis, and sets the cookie to expire.
//
// WARNING: This method should be considered deprecated since it is not exposed via the gorilla/sessions interface.
// Set session.Options.MaxAge = -1 and call Save instead. - July 18th, 2013.
func (s *RediStore) Delete(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	if _, err := s.DB.Del(r.Context(), s.keyPrefix+session.ID).Result(); err != nil {
		return ErrDeletingSession
	}
	// Set cookie to expire.
	options := *session.Options
	options.MaxAge = -1
	http.SetCookie(w, sessions.NewCookie(session.Name(), "", &options))
	// Clear session values.
	for k := range session.Values {
		delete(session.Values, k)
	}
	return nil
}

// ping does an internal ping against a server to check if it is alive.
func (s *RediStore) ping(ctx context.Context) (bool, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "RediStore.ping")
	defer span.End()
	data, err := s.DB.Ping(ctx).Result()
	if err != nil || data != "PONG" {
		return false, ErrRedis
	}
	return true, nil
}

// save stores the session in redis.
func (s *RediStore) save(ctx context.Context, session *sessions.Session) error {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "RediStore.save")
	defer span.End()
	b, err := s.serializer.Serialize(session)
	if err != nil {
		return ErrSerialization
	}
	if s.maxLength != 0 && len(b) > s.maxLength {
		return ErrStoreValueTooBig
	}
	age := time.Duration(session.Options.MaxAge) * time.Second
	if age == 0 {
		age = time.Duration(s.DefaultMaxAge) * time.Second
	}
	_, err = s.DB.SetEx(ctx, s.keyPrefix+session.ID, b, age).Result()
	if err != nil {
		return ErrSetExpiration
	}
	return nil
}

// load reads the session from redis.
// returns true if there is a sessoin data in DB.
func (s *RediStore) load(ctx context.Context, session *sessions.Session) (bool, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "RediStore.load")
	defer span.End()
	data, err := s.DB.Get(ctx, s.keyPrefix+session.ID).Result()
	if err != nil {
		return false, ErrGetSession
	}
	if len(data) == 0 {
		return false, nil // no data was associated with this key
	}
	err = s.serializer.Deserialize([]byte(data), session)
	if err != nil {
		return false, ErrDeserialization
	}
	return true, nil
}

// delete removes keys from redis if MaxAge<0.
func (s *RediStore) delete(ctx context.Context, session *sessions.Session) error {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "RediStore.delete")
	defer span.End()
	if _, err := s.DB.Del(ctx, s.keyPrefix+session.ID).Result(); err != nil {
		return ErrDeletingSession
	}
	return nil
}
