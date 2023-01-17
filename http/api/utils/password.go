package utils

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"k8s.io/klog/v2"
)

type argon2_params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
	salt        []byte
}

var (
	ErrInvalidHash         = errors.New("the encoded hash is not in the correct format")
	ErrIncompatibleVersion = errors.New("incompatible version of argon2")
)

func HashPassword(password string) string {
	var params = argon2_params{
		memory:      64 * 1024,
		iterations:  3,
		parallelism: 2,
		saltLength:  16,
		keyLength:   32,
		salt:        make([]byte, 16),
	}
	// Fill the salt with cryptographically secure random bytes.
	_, err := rand.Read(params.salt)
	if err != nil {
		klog.Errorf("HashPassword: %v", err)
	}

	bytes := argon2.IDKey([]byte(password), params.salt, params.iterations, params.memory, params.parallelism, params.keyLength)
	b64Salt := base64.RawStdEncoding.EncodeToString(params.salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(bytes)

	// Return a string using the standard encoded hash representation.
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, params.memory, params.iterations, params.parallelism, b64Salt, b64Hash)
}

func VerifyPassword(password, compareHash string) (bool, error) {
	vals := strings.Split(compareHash, "$")
	if len(vals) != 6 {
		return false, ErrInvalidHash
	}

	var version int
	_, err := fmt.Sscanf(vals[2], "v=%d", &version)
	if err != nil {
		return false, err
	}
	if version != argon2.Version {
		return false, ErrIncompatibleVersion
	}

	p := &argon2_params{}
	_, err = fmt.Sscanf(vals[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism)
	if err != nil {
		return false, err
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(vals[4])
	if err != nil {
		return false, err
	}
	p.saltLength = uint32(len(salt))

	hash, err := base64.RawStdEncoding.Strict().DecodeString(vals[5])
	if err != nil {
		return false, err
	}
	p.keyLength = uint32(len(hash))

	if err != nil {
		return false, err
	}

	// Derive the key from the other password using the same parameters.
	otherHash := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)

	// Check that the contents of the hashed passwords are identical. Note
	// that we are using the subtle.ConstantTimeCompare() function for this
	// to help prevent timing attacks.
	if subtle.ConstantTimeCompare([]byte(hash), otherHash) == 1 {
		return true, nil
	}
	return false, nil
}

const allowedChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const allowedNumbers = "0123456789"
const allowedSpecial = "!@#$%^&*-_"

func RandomPassword(length int, minNumbers, minSpecial int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, length)
	for i := range b {
		b[i] = allowedChars[rand.Intn(len(allowedChars))]
	}
	for i := 0; i < minNumbers; i++ {
		b[rand.Intn(length)] = allowedNumbers[rand.Intn(len(allowedNumbers))]
	}
	for i := 0; i < minSpecial; i++ {
		b[rand.Intn(length)] = allowedSpecial[rand.Intn(len(allowedSpecial))]
	}
	rand.Shuffle(length, func(i, j int) { b[i], b[j] = b[j], b[i] })
	return string(b)
}
