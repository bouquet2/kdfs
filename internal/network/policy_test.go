package network

import "testing"

func TestJoinEndpointFormatsIPv4AndIPv6(t *testing.T) {
	tests := []struct {
		name string
		host string
		port string
		want string
	}{
		{name: "ipv4", host: "10.0.0.5", port: "4420", want: "10.0.0.5:4420"},
		{name: "ipv6", host: "2001:db8::10", port: "4420", want: "[2001:db8::10]:4420"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JoinEndpoint(tt.host, tt.port); got != tt.want {
				t.Fatalf("JoinEndpoint() = %q", got)
			}
		})
	}
}

func TestSplitEndpointParsesCanonicalEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		host     string
		port     string
		wantErr  bool
	}{
		{name: "ipv4", endpoint: "10.0.0.5:4420", host: "10.0.0.5", port: "4420"},
		{name: "ipv6", endpoint: "[2001:db8::10]:4420", host: "2001:db8::10", port: "4420"},
		{name: "malformed", endpoint: "2001:db8::10:4420", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := SplitEndpoint(tt.endpoint)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if host != tt.host || port != tt.port {
				t.Fatalf("SplitEndpoint() = (%q, %q)", host, port)
			}
		})
	}
}

func TestResolvePolicyResolvesRuntimeTokens(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		policy  RolePolicies
		runtime RuntimeValues
		want    ResolvedPolicy
	}{
		{
			name: "engine ipv4 defaults",
			role: "engine",
			policy: RolePolicies{
				Default: Policy{BindAddress: "podIP", AdvertiseAddress: "podIP", PreferredFamily: FamilyAuto},
				Engine:  Policy{Port: "4420"},
			},
			runtime: RuntimeValues{PodIP: "10.0.0.5"},
			want:    ResolvedPolicy{BindAddress: "10.0.0.5", AdvertiseAddress: "10.0.0.5", Port: "4420", PreferredFamily: FamilyAuto, AdrFam: "IPv4"},
		},
		{
			name: "replica auto ipv6",
			role: "replica",
			policy: RolePolicies{
				Default: Policy{BindAddress: "podIP", AdvertiseAddress: "hostIP", PreferredFamily: FamilyAuto},
				Replica: Policy{Port: "4421"},
			},
			runtime: RuntimeValues{PodIP: "fd00::10", HostIP: "fd00::99", NodeName: "worker-1"},
			want:    ResolvedPolicy{BindAddress: "fd00::10", AdvertiseAddress: "fd00::99", Port: "4421", PreferredFamily: FamilyAuto, AdrFam: "IPv6"},
		},
		{
			name: "empty podIP falls back to empty values",
			role: "engine",
			policy: RolePolicies{
				Default: Policy{BindAddress: "podIP", AdvertiseAddress: "podIP", PreferredFamily: FamilyIPv4},
				Engine:  Policy{Port: "4420"},
			},
			want: ResolvedPolicy{BindAddress: "", AdvertiseAddress: "", Port: "4420", PreferredFamily: FamilyIPv4, AdrFam: "IPv4"},
		},
		{
			name: "literal ip preserves trimmed source text",
			role: "engine",
			policy: RolePolicies{
				Default: Policy{BindAddress: " 2001:db8::10 ", AdvertiseAddress: " 10.0.0.5 ", PreferredFamily: FamilyIPv6},
				Engine:  Policy{Port: " 4420 "},
			},
			want: ResolvedPolicy{BindAddress: "2001:db8::10", AdvertiseAddress: "10.0.0.5", Port: "4420", PreferredFamily: FamilyIPv6, AdrFam: "IPv6"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePolicy(tt.role, tt.policy, tt.runtime)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("ResolvePolicy() = %#v", got)
			}
		})
	}
}

func TestResolvePolicyRejectsUnknownToken(t *testing.T) {
	_, err := ResolvePolicy("engine", RolePolicies{
		Default: Policy{BindAddress: "mystery", AdvertiseAddress: "podIP", PreferredFamily: FamilyAuto},
		Engine:  Policy{Port: "4420"},
	}, RuntimeValues{PodIP: "10.0.0.5"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHTTPURLBracketsIPv6Hosts(t *testing.T) {
	if got := HTTPURL("http", "2001:db8::10", "9810", "/status"); got != "http://[2001:db8::10]:9810/status" {
		t.Fatalf("HTTPURL() = %q", got)
	}
}

func TestAddressFamilyForAddressDetectsIPv6(t *testing.T) {
	family, err := AddressFamilyForAddress("2001:db8::10", FamilyAuto)
	if err != nil {
		t.Fatal(err)
	}
	if family != "IPv6" {
		t.Fatalf("family = %q", family)
	}
}
