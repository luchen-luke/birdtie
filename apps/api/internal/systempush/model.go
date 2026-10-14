// Package systempush is a device transport for existing native Inbox records.
// A push carries only an opaque event and binding; it grants no object access.
package systempush

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
)

var (
	ErrInvalid     = errors.New("invalid system push input")
	ErrConflict    = errors.New("system push version changed")
	ErrNotFound    = errors.New("system push unavailable")
	ErrUnavailable = errors.New("system push service unavailable")
	ErrDisabled    = errors.New("system push disabled")
)

type Kind string

const (
	FCMToken             Kind  = "FCM_TOKEN"
	APNSToken            Kind  = "APNS_TOKEN"
	MaxVersion           int64 = 1 << 52
	RegistrationLifetime       = 30 * 24 * time.Hour
	EventLifetime              = 24 * time.Hour
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidID(s string) bool {
	return uuidPattern.MatchString(s) && s != "00000000-0000-0000-0000-000000000000"
}
func ProofDigest(s string) ([32]byte, error) {
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != s {
		return [32]byte{}, ErrInvalid
	}
	return sha256.Sum256(b), nil
}

type Access struct {
	identity.ProfileAccess
	InstallationID string
	ProofDigest    [32]byte
}

func ValidAccess(a Access) bool {
	return a.Actor.AccountType == "person" && ValidID(a.Actor.ID) && ValidID(a.InstallationID) && a.SessionDigest != ([32]byte{}) && a.ProofDigest != ([32]byte{})
}

// SecretAddress deliberately cannot serialize or format its contents.
type SecretAddress struct{ value string }

func Address(s string) SecretAddress               { return SecretAddress{value: s} }
func (SecretAddress) String() string               { return "[push-address-redacted]" }
func (SecretAddress) GoString() string             { return "[push-address-redacted]" }
func (SecretAddress) MarshalJSON() ([]byte, error) { return []byte(`"[push-address-redacted]"`), nil }
func (a SecretAddress) Bytes() []byte              { return []byte(a.value) }
func (a SecretAddress) Valid(k Kind) bool {
	if k == APNSToken {
		if len(a.value) < 64 || len(a.value) > 512 || len(a.value)%2 != 0 {
			return false
		}
		for _, c := range a.value {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
		return true
	}
	if k != FCMToken || len(a.value) < 32 || len(a.value) > 4096 {
		return false
	}
	for _, c := range a.value {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

type RegistrationInput struct {
	Schema           string
	OperationID      string
	ExpectedVersion  int64
	RegistrationKind Kind
	Platform         string
	Address          SecretAddress
	Permission       string
}

func ValidateRegistration(in RegistrationInput) error {
	if in.Schema != "system-push-registration-v1" || !ValidID(in.OperationID) || in.ExpectedVersion < 0 || in.ExpectedVersion >= MaxVersion || !in.Address.Valid(in.RegistrationKind) {
		return ErrInvalid
	}
	if in.RegistrationKind == FCMToken && in.Platform == "ANDROID" && in.Permission == "AUTHORIZED" {
		return nil
	}
	if in.RegistrationKind == APNSToken && in.Platform == "IOS" && (in.Permission == "AUTHORIZED" || in.Permission == "PROVISIONAL") {
		return nil
	}
	return ErrInvalid
}

type RevokeInput struct {
	Schema          string
	OperationID     string
	ExpectedVersion int64
}

func ValidateRevoke(in RevokeInput) error {
	if in.Schema != "system-push-revoke-v1" || !ValidID(in.OperationID) || in.ExpectedVersion < 0 || in.ExpectedVersion >= MaxVersion {
		return ErrInvalid
	}
	return nil
}

type Options struct {
	Schema            string `json:"schema"`
	Available         bool   `json:"available"`
	RegistrationKinds []Kind `json:"registrationKinds"`
	Proof             any    `json:"-"`
}
type Installation struct {
	Schema           string     `json:"schema"`
	InstallationID   string     `json:"installationId"`
	Version          int64      `json:"version"`
	BindingID        *string    `json:"bindingId"`
	Enabled          bool       `json:"enabled"`
	RegistrationKind *Kind      `json:"registrationKind"`
	RegisteredAt     *time.Time `json:"registeredAt"`
	ValidUntil       *time.Time `json:"validUntil"`
	Proof            any        `json:"-"`
}
type Operation struct {
	Schema          string    `json:"schema"`
	OperationID     string    `json:"operationId"`
	Operation       string    `json:"operation"`
	ExpectedVersion int64     `json:"expectedVersion"`
	ResultVersion   int64     `json:"resultVersion"`
	BindingID       *string   `json:"bindingId"`
	State           string    `json:"state"`
	CommittedAt     time.Time `json:"committedAt"`
	Proof           any       `json:"-"`
}
type OpenInput struct {
	EventID        string
	BindingID      string
	BindingVersion int64
}

func ValidOpen(in OpenInput) bool {
	return ValidID(in.EventID) && ValidID(in.BindingID) && in.BindingVersion > 0 && in.BindingVersion <= MaxVersion
}

type Open struct {
	Schema         string     `json:"schema"`
	EventID        string     `json:"eventId"`
	BindingID      string     `json:"bindingId"`
	BindingVersion int64      `json:"bindingVersion"`
	Item           inbox.Item `json:"item"`
	Proof          any        `json:"-"`
}
type HumanGateway interface {
	ReadOptions(context.Context, identity.ProfileAccess) (Options, error)
	ValidateOptions(context.Context, identity.ProfileAccess, Options) error
	ReadInstallation(context.Context, Access) (Installation, error)
	ValidateInstallation(context.Context, Access, Installation) error
	CheckAddress(context.Context, Access, AddressCheckInput) (AddressMatch, error)
	ValidateAddressMatch(context.Context, Access, AddressCheckInput, AddressMatch) error
	RegisterInstallation(context.Context, Access, RegistrationInput) (Operation, error)
	RevokeInstallation(context.Context, Access, RevokeInput) (Operation, error)
	ReadOperation(context.Context, Access, string) (Operation, error)
	ValidateOperation(context.Context, Access, Operation) error
	ResolveEvent(context.Context, Access, OpenInput) (Open, error)
	ValidateEvent(context.Context, Access, OpenInput, Open) error
}

type AddressCheckInput struct {
	Schema           string
	ExpectedVersion  int64
	RegistrationKind Kind
	Address          SecretAddress
}

func ValidateAddressCheck(in AddressCheckInput) error {
	if in.Schema != "system-push-address-check-v1" || in.ExpectedVersion <= 0 || in.ExpectedVersion > MaxVersion || !in.Address.Valid(in.RegistrationKind) {
		return ErrInvalid
	}
	return nil
}

type AddressMatch struct {
	Schema         string `json:"schema"`
	InstallationID string `json:"installationId"`
	BindingID      string `json:"bindingId"`
	Version        int64  `json:"version"`
	Matches        bool   `json:"matches"`
	Proof          any    `json:"-"`
}

func ValidAddressMatch(v AddressMatch) bool {
	return v.Schema == "system-push-address-match-v1" && ValidID(v.InstallationID) && ValidID(v.BindingID) && v.Version > 0 && v.Version <= MaxVersion
}

func ValidOptions(v Options) bool {
	if v.Schema != "system-push-options-v1" || v.RegistrationKinds == nil || v.Available != (len(v.RegistrationKinds) > 0) {
		return false
	}
	seen := map[Kind]bool{}
	for _, k := range v.RegistrationKinds {
		if (k != FCMToken && k != APNSToken) || seen[k] {
			return false
		}
		seen[k] = true
	}
	return true
}
func ValidInstallation(v Installation) bool {
	if v.Schema != "system-push-installation-v1" || !ValidID(v.InstallationID) || v.Version < 0 || v.Version > MaxVersion {
		return false
	}
	if !v.Enabled {
		return v.BindingID == nil && v.RegistrationKind == nil && v.RegisteredAt == nil && v.ValidUntil == nil
	}
	return v.Version > 0 && v.BindingID != nil && ValidID(*v.BindingID) && v.RegistrationKind != nil && (*v.RegistrationKind == FCMToken || *v.RegistrationKind == APNSToken) && v.RegisteredAt != nil && v.ValidUntil != nil && !v.RegisteredAt.IsZero() && v.ValidUntil.After(*v.RegisteredAt)
}
func ValidOperation(v Operation) bool {
	return v.Schema == "system-push-operation-v1" && ValidID(v.OperationID) && (v.Operation == "REGISTER" || v.Operation == "REVOKE") && v.ExpectedVersion >= 0 && v.ResultVersion == v.ExpectedVersion+1 && v.ResultVersion <= MaxVersion && v.State == "COMMITTED" && !v.CommittedAt.IsZero() && ((v.Operation == "REGISTER" && v.BindingID != nil && ValidID(*v.BindingID)) || (v.Operation == "REVOKE" && v.BindingID == nil))
}
func ValidResultID(s string) bool {
	if len(s) > 256 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || strings.ContainsRune(":_./%+-", c)) {
			return false
		}
	}
	return true
}
