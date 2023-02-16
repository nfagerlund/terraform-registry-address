package tfaddr

import (
	"fmt"
	"regexp"
	"strings"

	svchost "github.com/hashicorp/terraform-svchost"
)

// Provider encapsulates a single provider type. In the future this will be
// extended to include additional fields including Namespace and SourceHost
type Provider struct {
	Type      string
	Namespace string
	Hostname  svchost.Hostname
}

// DefaultProviderRegistryHost is the hostname used for provider addresses that do
// not have an explicit hostname.
const DefaultProviderRegistryHost = svchost.Hostname("registry.terraform.io")

// BuiltInProviderHost is the pseudo-hostname used for the "built-in" provider
// namespace. Built-in provider addresses must also have their namespace set
// to BuiltInProviderNamespace in order to be considered as built-in.
const BuiltInProviderHost = svchost.Hostname("terraform.io")

// BuiltInProviderNamespace is the provider namespace used for "built-in"
// providers. Built-in provider addresses must also have their hostname
// set to BuiltInProviderHost in order to be considered as built-in.
//
// The this namespace is literally named "builtin", in the hope that users
// who see FQNs containing this will be able to infer the way in which they are
// special, even if they haven't encountered the concept formally yet.
const BuiltInProviderNamespace = "builtin"

// UnknownProviderNamespace is the special string used to indicate
// unknown namespace, e.g. in "aws". This is equivalent to
// LegacyProviderNamespace for <0.12 style address. This namespace
// would never be produced by Terraform itself explicitly, it is
// only an internal placeholder.
const UnknownProviderNamespace = "?"

// LegacyProviderNamespace is the special string used in the Namespace field
// of type Provider to mark a legacy provider address. This special namespace
// value would normally be invalid, and can be used only when the hostname is
// DefaultProviderRegistryHost because that host owns the mapping from legacy name to
// FQN. This may be produced by Terraform 0.13.
const LegacyProviderNamespace = "-"

var providerNamePartPattern = regexp.MustCompile("^[0-9A-Za-z](?:[0-9A-Za-z-_]{0,62}[0-9A-Za-z])?$")

// String returns an FQN string, indended for use in machine-readable output.
func (pt Provider) String() string {
	if pt.IsZero() {
		panic("called String on zero-value addrs.Provider")
	}
	return pt.Hostname.ForDisplay() + "/" + pt.Namespace + "/" + pt.Type
}

// ForDisplay returns a user-friendly FQN string, simplified for readability. If
// the provider is using the default hostname, the hostname is omitted.
func (pt Provider) ForDisplay() string {
	if pt.IsZero() {
		panic("called ForDisplay on zero-value addrs.Provider")
	}

	if pt.Hostname == DefaultProviderRegistryHost {
		return pt.Namespace + "/" + pt.Type
	}
	return pt.Hostname.ForDisplay() + "/" + pt.Namespace + "/" + pt.Type
}

// NewProvider constructs a provider address from its parts, and normalizes
// the namespace and type parts to lowercase using unicode case folding rules
// so that resulting addrs.Provider values can be compared using standard
// Go equality rules (==).
//
// The hostname is given as a svchost.Hostname, which is required by the
// contract of that type to have already been normalized for equality testing.
//
// This function will panic if the given namespace or type name are not valid.
// When accepting namespace or type values from outside the program, use
// ParseProviderPart first to check that the given value is valid.
func NewProvider(hostname svchost.Hostname, namespace, typeName string) Provider {
	if namespace == LegacyProviderNamespace {
		// Legacy provider addresses must always be created via struct
		panic("attempt to create legacy provider address using NewProvider; use Provider{} instead")
	}
	if namespace == UnknownProviderNamespace {
		// Provider addresses with unknown namespace must always
		// be created via struct
		panic("attempt to create provider address with unknown namespace using NewProvider; use Provider{} instead")
	}
	if namespace == "" {
		// This case is already handled by MustParseProviderPart() below,
		// but we catch it early to provide more helpful message.
		panic("attempt to create provider address with empty namespace")
	}

	return Provider{
		Type:      MustParseProviderPart(typeName),
		Namespace: MustParseProviderPart(namespace),
		Hostname:  hostname,
	}
}

// LegacyString returns the provider type, which is frequently used
// interchangeably with provider name. This function can and should be removed
// when provider type is fully integrated. As a safeguard for future
// refactoring, this function panics if the Provider is not a legacy provider.
func (pt Provider) LegacyString() string {
	if pt.IsZero() {
		panic("called LegacyString on zero-value addrs.Provider")
	}
	if pt.Namespace != LegacyProviderNamespace && pt.Namespace != BuiltInProviderNamespace {
		panic(pt.String() + " cannot be represented as a legacy string")
	}
	return pt.Type
}

// IsZero returns true if the receiver is the zero value of addrs.Provider.
//
// The zero value is not a valid addrs.Provider and calling other methods on
// such a value is likely to either panic or otherwise misbehave.
func (pt Provider) IsZero() bool {
	return pt == Provider{}
}

// HasKnownNamespace returns true if the provider namespace is known
// (also if it is legacy namespace)
func (pt Provider) HasKnownNamespace() bool {
	return pt.Namespace != UnknownProviderNamespace
}

// IsBuiltIn returns true if the receiver is the address of a "built-in"
// provider. That is, a provider under terraform.io/builtin/ which is
// included as part of the Terraform binary itself rather than one to be
// installed from elsewhere.
//
// These are ignored by the provider installer because they are assumed to
// already be available without any further installation.
func (pt Provider) IsBuiltIn() bool {
	return pt.Hostname == BuiltInProviderHost && pt.Namespace == BuiltInProviderNamespace
}

// LessThan returns true if the receiver should sort before the other given
// address in an ordered list of provider addresses.
//
// This ordering is an arbitrary one just to allow deterministic results from
// functions that would otherwise have no natural ordering. It's subject
// to change in future.
func (pt Provider) LessThan(other Provider) bool {
	switch {
	case pt.Hostname != other.Hostname:
		return pt.Hostname < other.Hostname
	case pt.Namespace != other.Namespace:
		return pt.Namespace < other.Namespace
	default:
		return pt.Type < other.Type
	}
}

// IsLegacy returns true if the provider is a legacy-style provider
func (pt Provider) IsLegacy() bool {
	if pt.IsZero() {
		panic("called IsLegacy() on zero-value addrs.Provider")
	}

	return pt.Hostname == DefaultProviderRegistryHost && pt.Namespace == LegacyProviderNamespace

}

// Equals returns true if the receiver and other provider have the same attributes.
func (pt Provider) Equals(other Provider) bool {
	return pt == other
}

// ParseProviderSource parses the source attribute and returns a provider.
// This is intended primarily to parse the FQN-like strings returned by
// terraform-config-inspect.
//
// The following are valid source string formats:
//
//	name
//	namespace/name
//	hostname/namespace/name
//
// "name"-only format is parsed as -/name (i.e. legacy namespace)
// requiring further identification of the namespace via Registry API
func ParseProviderSource(str string) (Provider, error) {
	var ret Provider
	parts, err := parseSourceStringParts(str)
	if err != nil {
		return ret, err
	}

	name := parts[len(parts)-1]
	ret.Type = name
	ret.Hostname = DefaultProviderRegistryHost

	if len(parts) == 1 {
		return Provider{
			Hostname:  DefaultProviderRegistryHost,
			Namespace: UnknownProviderNamespace,
			Type:      name,
		}, nil
	}

	if len(parts) >= 2 {
		// the namespace is always the second-to-last part
		givenNamespace := parts[len(parts)-2]
		if givenNamespace == LegacyProviderNamespace {
			// For now we're tolerating legacy provider addresses until we've
			// finished updating the rest of the codebase to no longer use them,
			// or else we'd get errors round-tripping through legacy subsystems.
			ret.Namespace = LegacyProviderNamespace
		} else {
			namespace, err := ParseProviderPart(givenNamespace)
			if err != nil {
				return Provider{}, &ParserError{
					Summary: "Invalid provider namespace",
					Detail:  fmt.Sprintf(`Invalid provider namespace %q in source %q: %s"`, givenNamespace, str, err),
				}
			}
			ret.Namespace = namespace
		}
	}

	// Final Case: 3 parts
	if len(parts) == 3 {
		// the namespace is always the first part in a three-part source string
		givenHostname := parts[0]
		hn, err := svchost.ForComparison(givenHostname)
		if err != nil {
			return Provider{}, &ParserError{
				Summary: "Invalid provider source hostname",
				Detail:  fmt.Sprintf(`Invalid provider source hostname %q in source %q: %s"`, givenHostname, str, err),
			}
		}
		ret.Hostname = hn
	}

	if ret.Namespace == LegacyProviderNamespace && ret.Hostname != DefaultProviderRegistryHost {
		// Legacy provider addresses must always be on the default registry
		// host, because the default registry host decides what actual FQN
		// each one maps to.
		return Provider{}, &ParserError{
			Summary: "Invalid provider namespace",
			Detail:  "The legacy provider namespace \"-\" can be used only with hostname " + DefaultProviderRegistryHost.ForDisplay() + ".",
		}
	}

	// Due to how plugin executables are named and provider git repositories
	// are conventionally named, it's a reasonable and
	// apparently-somewhat-common user error to incorrectly use the
	// "terraform-provider-" prefix in a provider source address. There is
	// no good reason for a provider to have the prefix "terraform-" anyway,
	// so we've made that invalid from the start both so we can give feedback
	// to provider developers about the terraform- prefix being redundant
	// and give specialized feedback to folks who incorrectly use the full
	// terraform-provider- prefix to help them self-correct.
	const redundantPrefix = "terraform-"
	const userErrorPrefix = "terraform-provider-"
	if strings.HasPrefix(ret.Type, redundantPrefix) {
		if strings.HasPrefix(ret.Type, userErrorPrefix) {
			// Likely user error. We only return this specialized error if
			// whatever is after the prefix would otherwise be a
			// syntactically-valid provider type, so we don't end up advising
			// the user to try something that would be invalid for another
			// reason anyway.
			// (This is mainly just for robustness, because the validation
			// we already did above should've rejected most/all ways for
			// the suggestedType to end up invalid here.)
			suggestedType := ret.Type[len(userErrorPrefix):]
			if _, err := ParseProviderPart(suggestedType); err == nil {
				suggestedAddr := ret
				suggestedAddr.Type = suggestedType
				return Provider{}, &ParserError{
					Summary: "Invalid provider type",
					Detail:  fmt.Sprintf("Provider source %q has a type with the prefix %q, which isn't valid. Although that prefix is often used in the names of version control repositories for Terraform providers, provider source strings should not include it.\n\nDid you mean %q?", ret.ForDisplay(), userErrorPrefix, suggestedAddr.ForDisplay()),
				}
			}
		}
		// Otherwise, probably instead an incorrectly-named provider, perhaps
		// arising from a similar instinct to what causes there to be
		// thousands of Python packages on PyPI with "python-"-prefixed
		// names.
		return Provider{}, &ParserError{
			Summary: "Invalid provider type",
			Detail:  fmt.Sprintf("Provider source %q has a type with the prefix %q, which isn't allowed because it would be redundant to name a Terraform provider with that prefix. If you are the author of this provider, rename it to not include the prefix.", ret, redundantPrefix),
		}
	}

	return ret, nil
}

// MustParseProviderSource is a wrapper around ParseProviderSource that panics if
// it returns an error.
func MustParseProviderSource(raw string) Provider {
	p, err := ParseProviderSource(raw)
	if err != nil {
		panic(err)
	}
	return p
}

// ValidateProviderAddress returns error if the given address is not FQN,
// that is if it is missing any of the three components from
// hostname/namespace/name.
func ValidateProviderAddress(raw string) error {
	parts, err := parseSourceStringParts(raw)
	if err != nil {
		return err
	}

	if len(parts) != 3 {
		return &ParserError{
			Summary: "Invalid provider address format",
			Detail:  `Expected FQN in the format "hostname/namespace/name"`,
		}
	}

	p, err := ParseProviderSource(raw)
	if err != nil {
		return err
	}

	if !p.HasKnownNamespace() {
		return &ParserError{
			Summary: "Unknown provider namespace",
			Detail:  `Expected FQN in the format "hostname/namespace/name"`,
		}
	}

	if !p.IsLegacy() {
		return &ParserError{
			Summary: "Invalid legacy provider namespace",
			Detail:  `Expected FQN in the format "hostname/namespace/name"`,
		}
	}

	return nil
}

func parseSourceStringParts(str string) ([]string, error) {
	// split the source string into individual components
	parts := strings.Split(str, "/")
	if len(parts) == 0 || len(parts) > 3 {
		return nil, &ParserError{
			Summary: "Invalid provider source string",
			Detail:  `The "source" attribute must be in the format "[hostname/][namespace/]name"`,
		}
	}

	// check for an invalid empty string in any part
	for i := range parts {
		if parts[i] == "" {
			return nil, &ParserError{
				Summary: "Invalid provider source string",
				Detail:  `The "source" attribute must be in the format "[hostname/][namespace/]name"`,
			}
		}
	}

	// check the 'name' portion, which is always the last part
	givenName := parts[len(parts)-1]
	name, err := ParseProviderPart(givenName)
	if err != nil {
		return nil, &ParserError{
			Summary: "Invalid provider type",
			Detail:  fmt.Sprintf(`Invalid provider type %q in source %q: %s"`, givenName, str, err),
		}
	}
	parts[len(parts)-1] = name

	return parts, nil
}

// ParseProviderPart processes an addrs.Provider namespace or type string
// provided by an end-user, returning the same string if validation succeeds or
// an error if the string contains invalid characters.
//
// As per the implementation of HashiCorp's public and private Terraform
// registries, a provider part may be up to 64 characters long, can contain
// letters, digits, underscores, and dashes, must start with a letter, and must
// end with a letter or digit.
//
// In practice, these name segments are often more restricted. Most notably, the
// public registry requires namespaces to match a GitHub username (ruling out
// underscores). Also, some permitted provider names are hard to use because
// their resource type names will not be able to contain the provider type name
// and thus each resource will need an explicit provider address specified. (A
// real-world example of such a provider is the "google-beta" variant of the GCP
// provider, which has resource types that start with the "google_" prefix
// instead.)
func ParseProviderPart(given string) (string, error) {
	if len(given) == 0 {
		return "", fmt.Errorf("must have at least one character")
	}

	// We're going to process the given name using the same "IDNA" library we
	// use for the hostname portion, since it already implements the case
	// folding rules we want.
	//
	// The idna library doesn't expose individual label parsing directly, but
	// once we've verified it doesn't contain any dots we can just treat it
	// like a top-level domain for this library's purposes.
	if strings.ContainsRune(given, '.') {
		return "", fmt.Errorf("dots are not allowed")
	}

	if !providerNamePartPattern.MatchString(given) {
		return "", fmt.Errorf("must contain only letters, digits, dashes, and underscores, and may not use leading or trailing dashes or underscores")
	}

	return given, nil
}

// MustParseProviderPart is a wrapper around ParseProviderPart that panics if
// it returns an error.
func MustParseProviderPart(given string) string {
	result, err := ParseProviderPart(given)
	if err != nil {
		panic(err.Error())
	}
	return result
}
