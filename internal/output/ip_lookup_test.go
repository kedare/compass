package output

import (
	"net"
	"strings"
	"testing"

	"github.com/kedare/compass/internal/gcp"
	"github.com/stretchr/testify/require"
)

func TestBuildSubnetCIDRMap(t *testing.T) {
	t.Parallel()

	assocs := []gcp.IPAssociation{
		{
			Project:      "proj-1",
			Kind:         gcp.IPAssociationSubnet,
			Resource:     "subnet-a",
			Location:     "us-central1",
			Details:      "cidr=10.0.0.0/24",
			ResourceLink: "projects/proj-1/regions/us-central1/subnetworks/subnet-a",
		},
		{
			Project:  "proj-1",
			Kind:     gcp.IPAssociationInstanceInternal,
			Resource: "ignored",
		},
	}

	m := buildSubnetCIDRMap(assocs)
	require.Equal(t, "10.0.0.0/24", m["proj-1|subnet-a"])
	require.Equal(t, "10.0.0.0/24", m["proj-1|us-central1|subnet-a"])
}

func TestFormatIPWithMask(t *testing.T) {
	t.Parallel()

	subnetCIDRs := map[string]string{
		"proj-1|subnet-a":             "10.0.0.0/29",
		"proj-1|us-central1|subnet-a": "10.0.0.0/29",
	}

	subnetAssoc := gcp.IPAssociation{
		Kind:    gcp.IPAssociationSubnet,
		Details: "cidr=10.0.0.0/29",
	}

	require.Equal(t, "10.0.0.0/29", formatIPWithMask(subnetAssoc, subnetCIDRs))

	instanceAssoc := gcp.IPAssociation{
		Project:   "proj-1",
		Kind:      gcp.IPAssociationInstanceInternal,
		IPAddress: "10.0.0.2",
		Details:   "network=vpc-1,subnet=subnet-a",
	}

	require.Equal(t, "10.0.0.2/29", formatIPWithMask(instanceAssoc, subnetCIDRs))
}

func TestFormatAssociationPath(t *testing.T) {
	t.Parallel()

	assoc := gcp.IPAssociation{
		Project:  "proj-1",
		Kind:     gcp.IPAssociationInstanceInternal,
		Location: "us-central1-a",
		Details:  "network=vpc-1,subnet=subnet-a",
	}

	require.Equal(t, "proj-1 > vpc-1 > us-central1-a > subnet-a", formatAssociationPath(assoc))
}

func TestFormatDetailNoteVariants(t *testing.T) {
	t.Parallel()

	SetFormat("text")

	subnetAssoc := gcp.IPAssociation{
		Project:   "proj-1",
		Kind:      gcp.IPAssociationSubnet,
		Resource:  "subnet-a",
		IPAddress: "10.0.0.1",
		Details:   "cidr=10.0.0.0/24,range=PRIMARY,gateway=true",
	}

	note, ok := formatDetailNote(subnetAssoc, map[string]string{})
	require.True(t, ok)
	require.Contains(t, note, "Google Cloud default gateway")

	networkAssoc := gcp.IPAssociation{
		Project:   "proj-1",
		Kind:      gcp.IPAssociationInstanceInternal,
		IPAddress: "10.0.0.0",
		Details:   "subnet=subnet-a",
	}

	subnetCIDRs := map[string]string{
		"proj-1|subnet-a": "10.0.0.0/29",
	}

	note, ok = formatDetailNote(networkAssoc, subnetCIDRs)
	require.True(t, ok)
	require.Contains(t, note, "Subnet network address")
}

func TestUsableRangeString(t *testing.T) {
	t.Parallel()

	require.Equal(t, "10.0.0.1-10.0.0.254", usableRangeString("10.0.0.0/24"))
	require.Equal(t, "10.0.0.0-10.0.0.1", usableRangeString("10.0.0.0/31"))
	require.Equal(t, "10.0.0.5", usableRangeString("10.0.0.5/32"))
	require.Equal(t, "", usableRangeString("invalid"))
}

func TestIncrementDecrementAndCompareIPs(t *testing.T) {
	t.Parallel()

	ip := net.IPv4(10, 0, 0, 1).To4()
	require.Equal(t, ip.String(), net.IP(cloneIP(ip)).String())

	inc := incrementIP(ip)
	require.Equal(t, "10.0.0.2", net.IP(inc).String())

	dec := decrementIP(inc)
	require.Equal(t, ip.String(), net.IP(dec).String())

	require.Equal(t, 0, compareIPs(ip, net.IPv4(10, 0, 0, 1)))
	require.Equal(t, -1, compareIPs(net.IPv4(10, 0, 0, 0), net.IPv4(10, 0, 0, 1)))
	require.Equal(t, 1, compareIPs(net.IPv4(10, 0, 0, 2), net.IPv4(10, 0, 0, 1)))
}

func TestRenderPlainKeyValuesAndIndent(t *testing.T) {
	t.Parallel()

	pairs := []labelValue{
		{Label: "Resource", Value: "instance-1"},
		{Label: "Zone", Value: "us-central1-a"},
		{Label: "", Value: "ignored"},
	}

	rendered := renderPlainKeyValues(pairs)
	require.Contains(t, rendered, "Resource:")
	require.Contains(t, rendered, "Zone:")

	indented := indentBlock(rendered, "  ")
	require.True(t, strings.HasPrefix(indented, "  Resource"))
}

func TestDisplayIPLookupResultsText(t *testing.T) {
	t.Parallel()

	SetFormat("text")

	results := []gcp.IPAssociation{
		{
			Project:   "proj-1",
			Kind:      gcp.IPAssociationInstanceInternal,
			Resource:  "instance-1",
			Location:  "us-central1-a",
			IPAddress: "10.0.0.5",
			Details:   "network=vpc-1,subnet=subnet-a",
		},
		{
			Project:   "proj-1",
			Kind:      gcp.IPAssociationSubnet,
			Resource:  "subnet-a",
			Location:  "us-central1",
			IPAddress: "10.0.0.1",
			Details:   "cidr=10.0.0.0/24,range=PRIMARY,gateway=true",
		},
	}

	output := captureStdout(t, func() {
		require.NoError(t, DisplayIPLookupResults(results, "text"))
	})

	require.Contains(t, output, "Found 2 association(s)")
	require.Contains(t, output, "instance-1")
	require.Contains(t, output, "Subnet range")
	require.Contains(t, output, "subnet-a (10.0.0.0/24)")
}
