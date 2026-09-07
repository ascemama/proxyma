package users

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordHashAlgorithm = "argon2id"
	passwordHashVersion   = 19
	passwordHashMemory    = 19 * 1024
	passwordHashTime      = 2
	passwordHashThreads   = 1
	passwordHashSaltLen   = 16
	passwordHashKeyLen    = 32
)

// ResetPassword returns a copy of user with passwd_hash set from password.
func ResetPassword(user User, password []byte) (User, error) {
	hash, err := GeneratePasswordHash(password)
	if err != nil {
		return User{}, err
	}
	user.PasswdHash = hash
	return user, nil
}

// GeneratePasswordHash returns an Argon2id PHC string for password.
func GeneratePasswordHash(password []byte) (string, error) {
	if len(password) == 0 {
		return "", fmt.Errorf("password is required")
	}
	salt := make([]byte, passwordHashSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey(password, salt, passwordHashTime, passwordHashMemory, passwordHashThreads, passwordHashKeyLen)
	return encodeArgon2idPHC(salt, key), nil
}

// ValidatePasswordHash reports whether value is Proxyma's supported Argon2id PHC hash.
func ValidatePasswordHash(value string) error {
	_, _, err := parseArgon2idPHC(value)
	return err
}

// VerifyPassword checks password against an Proxyma Argon2id PHC hash.
func VerifyPassword(password []byte, hash string) error {
	params, storedKey, err := parseArgon2idPHC(hash)
	if err != nil {
		return err
	}
	key := argon2.IDKey(password, params.salt, params.time, params.memory, params.threads, uint32(len(storedKey)))
	if subtle.ConstantTimeCompare(key, storedKey) != 1 {
		return fmt.Errorf("password does not match")
	}
	return nil
}

type argon2idParams struct {
	memory  uint32
	time    uint32
	threads uint8
	salt    []byte
}

func encodeArgon2idPHC(salt []byte, key []byte) string {
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		passwordHashAlgorithm,
		passwordHashVersion,
		passwordHashMemory,
		passwordHashTime,
		passwordHashThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func parseArgon2idPHC(value string) (argon2idParams, []byte, error) {
	parts := strings.Split(value, "$")
	if len(parts) != 6 || parts[0] != "" {
		return argon2idParams{}, nil, fmt.Errorf("passwd_hash must be an Argon2id PHC string")
	}
	if parts[1] != passwordHashAlgorithm {
		return argon2idParams{}, nil, fmt.Errorf("passwd_hash algorithm must be %s", passwordHashAlgorithm)
	}
	if parts[2] != fmt.Sprintf("v=%d", passwordHashVersion) {
		return argon2idParams{}, nil, fmt.Errorf("passwd_hash version must be %d", passwordHashVersion)
	}
	params, err := parseArgon2idCost(parts[3])
	if err != nil {
		return argon2idParams{}, nil, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != passwordHashSaltLen {
		return argon2idParams{}, nil, fmt.Errorf("passwd_hash salt must be %d base64-encoded bytes", passwordHashSaltLen)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) != passwordHashKeyLen {
		return argon2idParams{}, nil, fmt.Errorf("passwd_hash key must be %d base64-encoded bytes", passwordHashKeyLen)
	}
	params.salt = salt
	return params, key, nil
}

func parseArgon2idCost(value string) (argon2idParams, error) {
	var params argon2idParams
	fields := strings.Split(value, ",")
	if len(fields) != 3 {
		return argon2idParams{}, fmt.Errorf("passwd_hash cost must include m, t, and p")
	}
	for _, field := range fields {
		keyValue := strings.SplitN(field, "=", 2)
		if len(keyValue) != 2 {
			return argon2idParams{}, fmt.Errorf("passwd_hash cost %q is invalid", field)
		}
		parsed, err := strconv.ParseUint(keyValue[1], 10, 32)
		if err != nil {
			return argon2idParams{}, fmt.Errorf("passwd_hash cost %q is invalid", field)
		}
		switch keyValue[0] {
		case "m":
			params.memory = uint32(parsed)
		case "t":
			params.time = uint32(parsed)
		case "p":
			if parsed > 255 {
				return argon2idParams{}, fmt.Errorf("passwd_hash parallelism is invalid")
			}
			params.threads = uint8(parsed)
		default:
			return argon2idParams{}, fmt.Errorf("passwd_hash cost %q is unsupported", keyValue[0])
		}
	}
	if params.memory != passwordHashMemory || params.time != passwordHashTime || params.threads != passwordHashThreads {
		return argon2idParams{}, fmt.Errorf("passwd_hash cost must be m=%d,t=%d,p=%d", passwordHashMemory, passwordHashTime, passwordHashThreads)
	}
	return params, nil
}
