package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidParameters  = errors.New("invalid Argon2id parameters")
	ErrInvalidEncodedHash = errors.New("invalid encoded password hash")
)

type Parameters struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltBytes   uint32
	KeyBytes    uint32
}

func DefaultParameters() Parameters {
	return Parameters{
		MemoryKiB:   64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltBytes:   16,
		KeyBytes:    32,
	}
}

type Argon2id struct {
	parameters Parameters
}

func NewArgon2id(parameters Parameters) (*Argon2id, error) {
	if err := validateParameters(parameters); err != nil {
		return nil, err
	}
	return &Argon2id{parameters: parameters}, nil
}

func (a *Argon2id) Hash(password string) (string, error) {
	salt := make([]byte, a.parameters.SaltBytes)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", errors.New("generate password salt")
	}
	derivedKey := argon2.IDKey(
		[]byte(password),
		salt,
		a.parameters.Iterations,
		a.parameters.MemoryKiB,
		a.parameters.Parallelism,
		a.parameters.KeyBytes,
	)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		a.parameters.MemoryKiB,
		a.parameters.Iterations,
		a.parameters.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derivedKey),
	), nil
}

func (a *Argon2id) Verify(password, encodedHash string) (bool, error) {
	parameters, salt, expectedKey, err := parseEncodedHash(encodedHash)
	if err != nil {
		return false, err
	}
	actualKey := argon2.IDKey(
		[]byte(password),
		salt,
		parameters.Iterations,
		parameters.MemoryKiB,
		parameters.Parallelism,
		uint32(len(expectedKey)),
	)
	return subtle.ConstantTimeCompare(actualKey, expectedKey) == 1, nil
}

func validateParameters(parameters Parameters) error {
	if parameters.MemoryKiB < 8*1024 || parameters.MemoryKiB > 256*1024 ||
		parameters.Iterations < 1 || parameters.Iterations > 10 ||
		parameters.Parallelism < 1 || parameters.Parallelism > 8 ||
		parameters.SaltBytes < 16 || parameters.SaltBytes > 64 ||
		parameters.KeyBytes < 16 || parameters.KeyBytes > 64 {
		return ErrInvalidParameters
	}
	return nil
}

func parseEncodedHash(encodedHash string) (Parameters, []byte, []byte, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Parameters{}, nil, nil, ErrInvalidEncodedHash
	}
	versionText, ok := strings.CutPrefix(parts[2], "v=")
	if !ok {
		return Parameters{}, nil, nil, ErrInvalidEncodedHash
	}
	version, err := strconv.Atoi(versionText)
	if err != nil || version != argon2.Version {
		return Parameters{}, nil, nil, ErrInvalidEncodedHash
	}

	parameterParts := strings.Split(parts[3], ",")
	if len(parameterParts) != 3 {
		return Parameters{}, nil, nil, ErrInvalidEncodedHash
	}
	memory, err := parseUint32Parameter(parameterParts[0], "m=")
	if err != nil {
		return Parameters{}, nil, nil, err
	}
	iterations, err := parseUint32Parameter(parameterParts[1], "t=")
	if err != nil {
		return Parameters{}, nil, nil, err
	}
	parallelismValue, err := parseUint32Parameter(parameterParts[2], "p=")
	if err != nil || parallelismValue > 255 {
		return Parameters{}, nil, nil, ErrInvalidEncodedHash
	}
	parallelism := uint8(parallelismValue)
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidEncodedHash
	}
	expectedKey, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidEncodedHash
	}
	parameters := Parameters{
		MemoryKiB:   memory,
		Iterations:  iterations,
		Parallelism: parallelism,
		SaltBytes:   uint32(len(salt)),
		KeyBytes:    uint32(len(expectedKey)),
	}
	if err := validateParameters(parameters); err != nil {
		return Parameters{}, nil, nil, ErrInvalidEncodedHash
	}
	return parameters, salt, expectedKey, nil
}

func parseUint32Parameter(value, prefix string) (uint32, error) {
	number, ok := strings.CutPrefix(value, prefix)
	if !ok || number == "" {
		return 0, ErrInvalidEncodedHash
	}
	parsed, err := strconv.ParseUint(number, 10, 32)
	if err != nil {
		return 0, ErrInvalidEncodedHash
	}
	return uint32(parsed), nil
}
