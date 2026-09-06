// Package kbclient is a thin HTTP client for the Kubrain api-server, carried by
// the Terraform provider. It intentionally duplicates the shape of
// cli/internal/client (which is internal to another module and can't be
// imported) — the wire contract is small and the duplication keeps the provider
// a standalone module. Only the endpoints the provider drives are implemented.
package kbclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client talks to a Kubrain api-server as one tenant.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New returns a client for base URL and bearer token. base has any trailing
// slash trimmed.
func New(base, token string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{}}
}

// NotFound reports whether an error came back from a 404 read, so resources can
// drop themselves from state instead of erroring.
type NotFound struct{ msg string }

func (e *NotFound) Error() string { return e.msg }

// IsNotFound reports whether err is a NotFound.
func IsNotFound(err error) bool {
	_, ok := err.(*NotFound)
	return ok
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := c.errMessage(method, path, resp.Status, respBody)
		if resp.StatusCode == http.StatusNotFound {
			return &NotFound{msg: msg}
		}
		return fmt.Errorf("%s", msg)
	}
	if out != nil && len(respBody) > 0 {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

func (c *Client) errMessage(method, path, status string, body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return fmt.Sprintf("api %s %s: %s", method, path, status)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, "", out)
}

func (c *Client) post(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, "", out)
}

func (c *Client) put(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodPut, path, nil, "", out)
}

func (c *Client) delete(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, "", nil)
}

// ---- VPC ----

// VPC mirrors the api-server VPC payload.
type VPC struct {
	Tenant  string `json:"tenant"`
	Name    string `json:"name"`
	Region  string `json:"region"`
	VNet    string `json:"vnet"`
	Zone    string `json:"zone"`
	VNI     int    `json:"vni"`
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
	State   string `json:"state"`
}

// EnsureVPC creates a VPC (idempotent) and returns it. region may be empty.
func (c *Client) EnsureVPC(ctx context.Context, name, region string) (*VPC, error) {
	q := url.Values{}
	q.Set("name", name)
	if region != "" {
		q.Set("region", region)
	}
	var v VPC
	return &v, c.post(ctx, "/v1/vpcs?"+q.Encode(), &v)
}

// GetVPC returns a VPC by name (NotFound if absent).
func (c *Client) GetVPC(ctx context.Context, name string) (*VPC, error) {
	var v VPC
	return &v, c.get(ctx, "/v1/vpcs/"+url.PathEscape(name), &v)
}

// DeleteVPC tears down a VPC.
func (c *Client) DeleteVPC(ctx context.Context, name string) error {
	return c.delete(ctx, "/v1/vpcs/"+url.PathEscape(name))
}

// ---- Cluster ----

// Cluster mirrors the api-server stored cluster record.
type Cluster struct {
	Tenant string `json:"tenant"`
	Name   string `json:"name"`
	VPC    string `json:"vpc"`
	Region string `json:"region"`
	Tier   string `json:"tier"`

	OpaqueID   string `json:"opaque_id"`
	NodeCIDR   string `json:"node_cidr"`
	EndpointIP string `json:"endpoint_ip"`
	Hostname   string `json:"hostname"`

	K8sVersion    string `json:"k8s_version"`
	TalosVersion  string `json:"talos_version"`
	RAMGiB        int    `json:"ram_gib"`
	VCPU          int    `json:"vcpu"`
	ControlPlanes int    `json:"control_planes"`
	Workers       int    `json:"workers"`
	NodesTotal    int    `json:"nodes_total"`

	Addons map[string]string `json:"addons,omitempty"`

	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// ClusterParams is the cluster-create request surface. Zero fields take the
// api-server's defaults (vpc=default, tier=dev, ram=2, k8s=pinned, workers=0).
type ClusterParams struct {
	Name    string
	VPC     string
	Tier    string
	RAMGiB  int
	Workers int
	K8s     string
}

func (p ClusterParams) query() url.Values {
	q := url.Values{}
	q.Set("name", p.Name)
	if p.VPC != "" {
		q.Set("vpc", p.VPC)
	}
	if p.Tier != "" {
		q.Set("tier", p.Tier)
	}
	if p.RAMGiB > 0 {
		q.Set("ram", strconv.Itoa(p.RAMGiB))
	}
	if p.Workers > 0 {
		q.Set("nodes", strconv.Itoa(p.Workers))
	}
	if p.K8s != "" {
		q.Set("k8s", p.K8s)
	}
	return q
}

// CreateCluster provisions a cluster; the api-server reserves the allocation and
// provisions asynchronously, returning the record in state "provisioning".
func (c *Client) CreateCluster(ctx context.Context, p ClusterParams) (*Cluster, error) {
	var rec Cluster
	return &rec, c.post(ctx, "/v1/clusters?"+p.query().Encode(), &rec)
}

// GetCluster returns a cluster's status by name (NotFound if absent).
func (c *Client) GetCluster(ctx context.Context, name string) (*Cluster, error) {
	var rec Cluster
	return &rec, c.get(ctx, "/v1/clusters/"+url.PathEscape(name), &rec)
}

// DeleteCluster tears down a cluster (cascades to its VMs, LB IPs, volumes).
func (c *Client) DeleteCluster(ctx context.Context, name string) error {
	return c.delete(ctx, "/v1/clusters/"+url.PathEscape(name))
}

// ScaleCluster changes a cluster's worker count.
func (c *Client) ScaleCluster(ctx context.Context, name string, workers int) (*Cluster, error) {
	var rec Cluster
	q := url.Values{"workers": {strconv.Itoa(workers)}}
	return &rec, c.post(ctx, "/v1/clusters/"+url.PathEscape(name)+"/scale?"+q.Encode(), &rec)
}

// ResizeCluster changes a cluster's per-worker RAM (GiB).
func (c *Client) ResizeCluster(ctx context.Context, name string, ramGiB int) (*Cluster, error) {
	var rec Cluster
	q := url.Values{"ram": {strconv.Itoa(ramGiB)}}
	return &rec, c.post(ctx, "/v1/clusters/"+url.PathEscape(name)+"/resize?"+q.Encode(), &rec)
}

// UpgradeCluster upgrades a cluster's Kubernetes version (async roll).
func (c *Client) UpgradeCluster(ctx context.Context, name, k8s string) (*Cluster, error) {
	var rec Cluster
	q := url.Values{"k8s": {k8s}}
	return &rec, c.post(ctx, "/v1/clusters/"+url.PathEscape(name)+"/upgrade?"+q.Encode(), &rec)
}

// ---- Bucket ----

// Bucket mirrors the api-server object-storage bucket payload.
type Bucket struct {
	Name      string `json:"name"`
	Bucket    string `json:"bucket"`
	Region    string `json:"region"`
	Endpoint  string `json:"endpoint"`
	Public    bool   `json:"public"`
	PublicURL string `json:"public_url,omitempty"`
}

// BucketCredentials is the tenant's Kubrain-issued S3 key pair.
type BucketCredentials struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

// CreateBucket creates a bucket (idempotent). public is fixed at creation.
func (c *Client) CreateBucket(ctx context.Context, name string, public bool) (*Bucket, error) {
	q := url.Values{}
	q.Set("name", name)
	if public {
		q.Set("public", "true")
	}
	var b Bucket
	return &b, c.post(ctx, "/v1/buckets?"+q.Encode(), &b)
}

// GetBucket returns one bucket (NotFound if absent).
func (c *Client) GetBucket(ctx context.Context, name string) (*Bucket, error) {
	var b Bucket
	return &b, c.get(ctx, "/v1/buckets/"+url.PathEscape(name), &b)
}

// DeleteBucket removes an empty bucket (non-empty is refused with an error).
func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	return c.delete(ctx, "/v1/buckets/"+url.PathEscape(name))
}

// GetBucketCredentials returns the tenant's S3 key pair (stable across calls).
func (c *Client) GetBucketCredentials(ctx context.Context) (*BucketCredentials, error) {
	var bc BucketCredentials
	return &bc, c.get(ctx, "/v1/buckets/credentials", &bc)
}

// ---- DNS ----

// DNSRecord mirrors one RRset.
type DNSRecord struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	TTL    uint32   `json:"ttl"`
	Values []string `json:"values"`
}

// Zone mirrors a hosted DNS zone, including its delegated nameservers.
type Zone struct {
	Tenant      string      `json:"tenant"`
	Name        string      `json:"name"`
	Serial      uint32      `json:"serial"`
	Records     []DNSRecord `json:"records"`
	NameServers []string    `json:"name_servers"`
}

// CreateZone claims a domain (idempotent) and returns the NS set to delegate.
func (c *Client) CreateZone(ctx context.Context, domain string) (*Zone, error) {
	var z Zone
	return &z, c.post(ctx, "/v1/zones?domain="+url.QueryEscape(domain), &z)
}

// GetZone returns a hosted zone (NotFound if absent).
func (c *Client) GetZone(ctx context.Context, domain string) (*Zone, error) {
	var z Zone
	return &z, c.get(ctx, "/v1/zones/"+url.PathEscape(domain), &z)
}

// DeleteZone removes the zone and all its records.
func (c *Client) DeleteZone(ctx context.Context, domain string) error {
	return c.delete(ctx, "/v1/zones/"+url.PathEscape(domain))
}

// SetRecord replaces the RRset for (name, type) — create and update are the same
// idempotent call. ttl 0 means the server default.
func (c *Client) SetRecord(ctx context.Context, domain, name, rtype string, values []string, ttl int) (*DNSRecord, error) {
	q := url.Values{}
	q.Set("name", name)
	q.Set("type", rtype)
	if ttl > 0 {
		q.Set("ttl", strconv.Itoa(ttl))
	}
	for _, v := range values {
		q.Add("value", v)
	}
	var rec DNSRecord
	return &rec, c.put(ctx, "/v1/zones/"+url.PathEscape(domain)+"/records?"+q.Encode(), &rec)
}

// GetRecord reads a single RRset for (name, type) from a zone, or NotFound.
func (c *Client) GetRecord(ctx context.Context, domain, name, rtype string) (*DNSRecord, error) {
	z, err := c.GetZone(ctx, domain)
	if err != nil {
		return nil, err
	}
	for i := range z.Records {
		if z.Records[i].Name == name && strings.EqualFold(z.Records[i].Type, rtype) {
			return &z.Records[i], nil
		}
	}
	return nil, &NotFound{msg: fmt.Sprintf("dns record %s %s not found in zone %s", name, rtype, domain)}
}

// DeleteRecord removes the RRset for (name, type). Idempotent.
func (c *Client) DeleteRecord(ctx context.Context, domain, name, rtype string) error {
	q := url.Values{}
	q.Set("name", name)
	q.Set("type", rtype)
	return c.delete(ctx, "/v1/zones/"+url.PathEscape(domain)+"/records?"+q.Encode())
}

// ---- Registry ----

// Registry describes the tenant's hosted container registry coordinates.
type Registry struct {
	Host      string `json:"host"`
	Namespace string `json:"namespace"`
	Username  string `json:"username"`
	Login     string `json:"login"`
}

// GetRegistry returns the tenant's registry coordinates.
func (c *Client) GetRegistry(ctx context.Context) (*Registry, error) {
	var r Registry
	return &r, c.get(ctx, "/v1/registry", &r)
}
