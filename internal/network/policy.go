package network

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

type AddressFamily string

const (
	FamilyAuto AddressFamily = "Auto"
	FamilyIPv4 AddressFamily = "IPv4"
	FamilyIPv6 AddressFamily = "IPv6"
)

type Policy struct {
	BindAddress      string        `json:"bindAddress,omitempty"`
	AdvertiseAddress string        `json:"advertiseAddress,omitempty"`
	Port             string        `json:"port,omitempty"`
	PreferredFamily  AddressFamily `json:"preferredFamily,omitempty"`
}

type RolePolicies struct {
	Default Policy `json:"default"`
	Engine  Policy `json:"engine,omitempty"`
	Replica Policy `json:"replica,omitempty"`
}

type RuntimeValues struct {
	PodIP    string
	HostIP   string
	NodeName string
}

type ResolvedPolicy struct {
	BindAddress      string
	AdvertiseAddress string
	Port             string
	PreferredFamily  AddressFamily
	AdrFam           string
}

func JoinEndpoint(host, port string) string {
	return net.JoinHostPort(host, port)
}

func SplitEndpoint(endpoint string) (string, string, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", "", fmt.Errorf("split endpoint %q: %w", endpoint, err)
	}
	return host, port, nil
}

func HTTPURL(scheme, host, port, path string) string {
	return (&url.URL{Scheme: scheme, Host: JoinEndpoint(host, port), Path: path}).String()
}

func ResolvePolicy(role string, policies RolePolicies, runtime RuntimeValues) (ResolvedPolicy, error) {
	policy := policies.Default
	switch role {
	case "engine":
		policy = mergePolicy(policy, policies.Engine)
	case "replica":
		policy = mergePolicy(policy, policies.Replica)
	}

	bindAddress, err := resolveAddressToken(policy.BindAddress, runtime, runtime.PodIP)
	if err != nil {
		return ResolvedPolicy{}, fmt.Errorf("resolve bindAddress: %w", err)
	}
	advertiseAddress, err := resolveAddressToken(policy.AdvertiseAddress, runtime, bindAddress)
	if err != nil {
		return ResolvedPolicy{}, fmt.Errorf("resolve advertiseAddress: %w", err)
	}
	port := strings.TrimSpace(policy.Port)
	if port == "" {
		return ResolvedPolicy{}, fmt.Errorf("port is required for role %q", role)
	}
	preferredFamily := normalizeFamily(policy.PreferredFamily)
	adrFam, err := resolveAdrFam(bindAddress, preferredFamily)
	if err != nil {
		return ResolvedPolicy{}, err
	}

	return ResolvedPolicy{
		BindAddress:      bindAddress,
		AdvertiseAddress: advertiseAddress,
		Port:             port,
		PreferredFamily:  preferredFamily,
		AdrFam:           adrFam,
	}, nil
}

func AddressFamilyForAddress(address string, family AddressFamily) (string, error) {
	return resolveAdrFam(address, normalizeFamily(family))
}

func mergePolicy(base, override Policy) Policy {
	if override.BindAddress != "" {
		base.BindAddress = override.BindAddress
	}
	if override.AdvertiseAddress != "" {
		base.AdvertiseAddress = override.AdvertiseAddress
	}
	if override.Port != "" {
		base.Port = override.Port
	}
	if override.PreferredFamily != "" {
		base.PreferredFamily = override.PreferredFamily
	}
	return base
}

func resolveAddressToken(value string, runtime RuntimeValues, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "podIP"
	}

	switch value {
	case "podIP":
		if runtime.PodIP != "" {
			return runtime.PodIP, nil
		}
		return fallback, nil
	case "hostIP":
		if runtime.HostIP == "" {
			return "", fmt.Errorf("hostIP requested but not available")
		}
		return runtime.HostIP, nil
	case "nodeName":
		if runtime.NodeName == "" {
			return "", fmt.Errorf("nodeName requested but not available")
		}
		return runtime.NodeName, nil
	}

	if ip := net.ParseIP(value); ip != nil {
		return value, nil
	}
	return "", fmt.Errorf("unsupported address token %q", value)
}

func resolveAdrFam(address string, family AddressFamily) (string, error) {
	if family != FamilyAuto {
		return string(family), nil
	}
	if ip := net.ParseIP(address); ip != nil {
		if ip.To4() == nil {
			return string(FamilyIPv6), nil
		}
		return string(FamilyIPv4), nil
	}
	return "", fmt.Errorf("cannot derive address family from non-IP bind address %q", address)
}

func normalizeFamily(family AddressFamily) AddressFamily {
	switch family {
	case "", FamilyAuto:
		return FamilyAuto
	case FamilyIPv4:
		return FamilyIPv4
	case FamilyIPv6:
		return FamilyIPv6
	default:
		return FamilyAuto
	}
}
