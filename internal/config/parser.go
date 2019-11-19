package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/NetApp/nks-on-prem-ipam/pkg/ipam"
	"github.com/NetApp/nks-on-prem-ipam/pkg/ipam/factory"
	"github.com/mikioh/ipaddr"
	"gopkg.in/yaml.v2"
	"k8s.io/client-go/kubernetes"
	"net"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type Parser struct {
	k8s kubernetes.Interface
}

func NewParser(k8s kubernetes.Interface) Parser {
	return Parser{
		k8s: k8s,
	}
}

// Parse loads and validates a Config from bs.
func (cp Parser) Parse(bs []byte) (*Config, error) {
	var raw configFile
	if err := yaml.UnmarshalStrict(bs, &raw); err != nil {
		return nil, fmt.Errorf("could not parse secret: %s", err)
	}

	cfg := &Config{Pools: map[string]*Pool{}}
	for i, p := range raw.Peers {
		peer, err := cp.parsePeer(p)
		if err != nil {
			return nil, fmt.Errorf("parsing peer #%d: %s", i+1, err)
		}
		cfg.Peers = append(cfg.Peers, peer)
	}

	communities := map[string]uint32{}
	for n, v := range raw.BGPCommunities {
		c, err := parseCommunity(v)
		if err != nil {
			return nil, fmt.Errorf("parsing community %q: %s", n, err)
		}
		communities[n] = c
	}

	var allCIDRs []*net.IPNet
	for i, p := range raw.Pools {
		pool, err := cp.parseAddressPool(p, communities)
		if err != nil {
			return nil, fmt.Errorf("parsing address pool #%d: %s", i+1, err)
		}

		// Check that the pool isn't already defined
		if cfg.Pools[p.Name] != nil {
			return nil, fmt.Errorf("duplicate definition of pool %q", p.Name)
		}

		// Check that all specified CIDR ranges are non-overlapping.
		for _, cidr := range pool.CIDR {
			for _, m := range allCIDRs {
				if cidrsOverlap(cidr, m) {
					return nil, fmt.Errorf("CIDR %q in pool %q overlaps with already defined CIDR %q", cidr, p.Name, m)
				}
			}
			allCIDRs = append(allCIDRs, cidr)
		}

		cfg.Pools[p.Name] = pool
	}

	return cfg, nil
}

func (cp Parser) parseNodeSelector(ns *nodeSelector) (labels.Selector, error) {
	if len(ns.MatchLabels)+len(ns.MatchExpressions) == 0 {
		return labels.Everything(), nil
	}

	// Convert to a metav1.LabelSelector so we can use the fancy
	// parsing function to create a Selector.
	//
	// Why not use metav1.LabelSelector in the raw secret object?
	// Because metav1.LabelSelector doesn't have yaml tag
	// specifications.
	sel := &metav1.LabelSelector{
		MatchLabels: ns.MatchLabels,
	}
	for _, req := range ns.MatchExpressions {
		sel.MatchExpressions = append(sel.MatchExpressions, metav1.LabelSelectorRequirement{
			Key:      req.Key,
			Operator: metav1.LabelSelectorOperator(req.Operator),
			Values:   req.Values,
		})
	}

	return metav1.LabelSelectorAsSelector(sel)
}

func (cp Parser) parseHoldTime(ht string) (time.Duration, error) {
	if ht == "" {
		return 90 * time.Second, nil
	}
	d, err := time.ParseDuration(ht)
	if err != nil {
		return 0, fmt.Errorf("invalid hold time %q: %s", ht, err)
	}
	rounded := time.Duration(int(d.Seconds())) * time.Second
	if rounded != 0 && rounded < 3*time.Second {
		return 0, fmt.Errorf("invalid hold time %q: must be 0 or >=3s", ht)
	}
	return rounded, nil
}

func (cp Parser) parsePeer(p peer) (*Peer, error) {
	if p.MyASN == 0 {
		return nil, errors.New("missing local ASN")
	}
	if p.ASN == 0 {
		return nil, errors.New("missing peer ASN")
	}
	ip := net.ParseIP(p.Addr)
	if ip == nil {
		return nil, fmt.Errorf("invalid peer IP %q", p.Addr)
	}
	holdTime, err := cp.parseHoldTime(p.HoldTime)
	if err != nil {
		return nil, err
	}
	port := uint16(179)
	if p.Port != 0 {
		port = p.Port
	}
	// Ideally we would set a default RouterID here, instead of having
	// to do it elsewhere in the code. Unfortunately, we don't know
	// the node IP here.
	var routerID net.IP
	if p.RouterID != "" {
		routerID = net.ParseIP(p.RouterID)
		if routerID == nil {
			return nil, fmt.Errorf("invalid router ID %q", p.RouterID)
		}
	}

	// We use a non-pointer in the raw json object, so that if the
	// user doesn't provide a node selector, we end up with an empty,
	// but non-nil selector, which means "select everything".
	var nodeSels []labels.Selector
	if len(p.NodeSelectors) == 0 {
		nodeSels = []labels.Selector{labels.Everything()}
	} else {
		for _, sel := range p.NodeSelectors {
			nodeSel, err := cp.parseNodeSelector(&sel)
			if err != nil {
				return nil, fmt.Errorf("parsing node selector: %s", err)
			}
			nodeSels = append(nodeSels, nodeSel)
		}
	}

	var password string
	if p.Password != "" {
		password = p.Password
	}
	return &Peer{
		MyASN:         p.MyASN,
		ASN:           p.ASN,
		Addr:          ip,
		Port:          port,
		HoldTime:      holdTime,
		RouterID:      routerID,
		NodeSelectors: nodeSels,
		Password:      password,
	}, nil
}

func (cp Parser) parseAddressPool(p addressPool, bgpCommunities map[string]uint32) (*Pool, error) {
	if p.Name == "" {
		return nil, errors.New("missing pool name")
	}

	ret := &Pool{
		Protocol:      p.Protocol,
		AvoidBuggyIPs: p.AvoidBuggyIPs,
		AutoAssign:    true,
	}

	if p.AutoAssign != nil {
		ret.AutoAssign = *p.AutoAssign
	}

	if len(p.Addresses) == 0 && p.Protocol != IPAM {
		return nil, errors.New("pool has no prefixes defined")
	}
	for _, cidr := range p.Addresses {
		nets, err := parseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q in pool %q: %s", cidr, p.Name, err)
		}
		ret.CIDR = append(ret.CIDR, nets...)
	}

	switch ret.Protocol {
	case Layer2:
		if len(p.BGPAdvertisements) > 0 {
			return nil, errors.New("cannot have bgp-advertisements configuration element in a layer2 address pool")
		}
	case BGP:
		ads, err := parseBGPAdvertisements(p.BGPAdvertisements, ret.CIDR, bgpCommunities)
		if err != nil {
			return nil, fmt.Errorf("parsing BGP communities: %s", err)
		}
		ret.BGPAdvertisements = ads
	case IPAM:
		config, err := cp.loadIPAMConfig(p.IPAM)
		if err != nil {
			return nil, fmt.Errorf("parsing ipam agent secret: %w", err)
		}

		agent, err := factory.GetAgent(config)
		if err != nil {
			return nil, fmt.Errorf("unable to create ipam agent: %w", err)
		}
		ret.IPAM = agent
	case "":
		return nil, errors.New("address pool is missing the protocol field")
	default:
		return nil, fmt.Errorf("unknown protocol %q", ret.Protocol)
	}

	return ret, nil
}

func parseBGPAdvertisements(ads []bgpAdvertisement, cidrs []*net.IPNet, communities map[string]uint32) ([]*BGPAdvertisement, error) {
	if len(ads) == 0 {
		return []*BGPAdvertisement{
			{
				AggregationLength: 32,
				LocalPref:         0,
				Communities:       map[uint32]bool{},
			},
		}, nil
	}

	var ret []*BGPAdvertisement
	for _, rawAd := range ads {
		ad := &BGPAdvertisement{
			AggregationLength: 32,
			LocalPref:         0,
			Communities:       map[uint32]bool{},
		}

		if rawAd.AggregationLength != nil {
			ad.AggregationLength = *rawAd.AggregationLength
		}
		if ad.AggregationLength > 32 {
			return nil, fmt.Errorf("invalid aggregation length %q", ad.AggregationLength)
		}
		for _, cidr := range cidrs {
			o, _ := cidr.Mask.Size()
			if ad.AggregationLength < o {
				return nil, fmt.Errorf("invalid aggregation length %d: prefix %q in this pool is more specific than the aggregation length", ad.AggregationLength, cidr)
			}
		}

		if rawAd.LocalPref != nil {
			ad.LocalPref = *rawAd.LocalPref
		}

		for _, c := range rawAd.Communities {
			if v, ok := communities[c]; ok {
				ad.Communities[v] = true
			} else {
				v, err := parseCommunity(c)
				if err != nil {
					return nil, fmt.Errorf("invalid community %q in BGP advertisement: %s", c, err)
				}
				ad.Communities[v] = true
			}
		}

		ret = append(ret, ad)
	}

	return ret, nil
}

func parseCommunity(c string) (uint32, error) {
	fs := strings.Split(c, ":")
	if len(fs) != 2 {
		return 0, fmt.Errorf("invalid community string %q", c)
	}
	a, err := strconv.ParseUint(fs[0], 10, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid first section of community %q: %s", fs[0], err)
	}
	b, err := strconv.ParseUint(fs[1], 10, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid second section of community %q: %s", fs[0], err)
	}

	return (uint32(a) << 16) + uint32(b), nil
}

func parseCIDR(cidr string) ([]*net.IPNet, error) {
	if !strings.Contains(cidr, "-") {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q", cidr)
		}
		return []*net.IPNet{n}, nil
	}

	fs := strings.SplitN(cidr, "-", 2)
	if len(fs) != 2 {
		return nil, fmt.Errorf("invalid IP range %q", cidr)
	}
	start := net.ParseIP(fs[0])
	if start == nil {
		return nil, fmt.Errorf("invalid IP range %q: invalid start IP %q", cidr, fs[0])
	}
	end := net.ParseIP(fs[1])
	if end == nil {
		return nil, fmt.Errorf("invalid IP range %q: invalid end IP %q", cidr, fs[1])
	}

	var ret []*net.IPNet
	for _, pfx := range ipaddr.Summarize(start, end) {
		n := &net.IPNet{
			IP:   pfx.IP,
			Mask: pfx.Mask,
		}
		ret = append(ret, n)
	}
	return ret, nil
}

func (cp Parser) loadIPAMConfig(i ipamConfig) (*ipam.Config, error) {
	if i.SecretName == "" {
		return nil, fmt.Errorf("ipam secret secret name missing")
	}

	if i.Namespace == "" {
		return nil, fmt.Errorf("ipam secret secret namespace missing")
	}

	secretKey := "config.json"
	if i.SecretKey != "" {
		secretKey = i.SecretKey
	}

	secret, err := cp.k8s.CoreV1().Secrets(i.Namespace).Get(i.SecretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting ipam secret secret %s in namespace %s, %w", i.SecretName, i.Namespace, err)
	}

	configBytes, ok := secret.Data[secretKey]
	if !ok {
		return nil, fmt.Errorf("ipam secret missing from secret %s in namespace %s", i.SecretKey, i.Namespace)
	}

	cfg := &ipam.Config{}
	err = json.Unmarshal(configBytes, cfg)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal ipam secret, %w", err)
	}

	return cfg, nil
}
