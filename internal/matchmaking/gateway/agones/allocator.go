package agones

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	platformid "github.com/ccKccK-JF/MatchMind/internal/platform/id"
)

var (
	ErrInvalidConfig      = errors.New("invalid Agones allocator configuration")
	ErrAllocationRejected = errors.New("Agones allocation was not allocated")
	ErrInvalidResponse    = errors.New("invalid Agones API response")
)

const responseLimit = 2 << 20

type Config struct {
	APIURL         string
	Namespace      string
	BearerToken    string
	GameLabelKey   string
	GameLabelValue string
	RegionLabelKey string
	HTTPClient     *http.Client
	TokenGenerator platformid.Generator
}

type Allocator struct {
	apiURL         string
	namespace      string
	bearerToken    string
	gameLabelKey   string
	gameLabelValue string
	regionLabelKey string
	client         *http.Client
	tokenGenerator platformid.Generator
}

func NewAllocator(config Config) (*Allocator, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(config.APIURL))
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, ErrInvalidConfig
	}
	config.Namespace = strings.TrimSpace(config.Namespace)
	config.GameLabelKey = strings.TrimSpace(config.GameLabelKey)
	config.GameLabelValue = strings.TrimSpace(config.GameLabelValue)
	config.RegionLabelKey = strings.TrimSpace(config.RegionLabelKey)
	if config.Namespace == "" || config.GameLabelKey == "" || config.GameLabelValue == "" || config.RegionLabelKey == "" {
		return nil, ErrInvalidConfig
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if config.TokenGenerator == nil {
		config.TokenGenerator = platformid.UUID
	}
	return &Allocator{
		apiURL: strings.TrimRight(parsedURL.String(), "/"), namespace: config.Namespace,
		bearerToken: strings.TrimSpace(config.BearerToken), gameLabelKey: config.GameLabelKey,
		gameLabelValue: config.GameLabelValue, regionLabelKey: config.RegionLabelKey,
		client: config.HTTPClient, tokenGenerator: config.TokenGenerator,
	}, nil
}

func NewHTTPClient(caFile string, insecureSkipVerify bool, timeout time.Duration) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecureSkipVerify} // #nosec G402 -- explicit development-only configuration.
	if caFile = strings.TrimSpace(caFile); caFile != "" {
		certificate, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read Agones CA: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(certificate) {
			return nil, ErrInvalidConfig
		}
		tlsConfig.RootCAs = roots
	}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{TLSClientConfig: tlsConfig}}, nil
}

func LoadBearerToken(value, file string) (string, error) {
	if value = strings.TrimSpace(value); value != "" {
		return value, nil
	}
	content, err := os.ReadFile(strings.TrimSpace(file))
	if err != nil {
		return "", fmt.Errorf("read Agones bearer token: %w", err)
	}
	value = strings.TrimSpace(string(content))
	if value == "" {
		return "", ErrInvalidConfig
	}
	return value, nil
}

func (a *Allocator) Capacities(ctx context.Context) ([]application.RegionCapacity, error) {
	selector := url.Values{"labelSelector": {a.gameLabelKey + "=" + a.gameLabelValue}}
	endpoint := fmt.Sprintf("%s/apis/agones.dev/v1/namespaces/%s/fleets?%s", a.apiURL, url.PathEscape(a.namespace), selector.Encode())
	var response fleetList
	if err := a.doJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return nil, err
	}
	combined := make(map[string]int)
	for _, fleet := range response.Items {
		region := strings.ToLower(strings.TrimSpace(fleet.Metadata.Labels[a.regionLabelKey]))
		if region != "" && fleet.Status.ReadyReplicas > 0 {
			combined[region] += fleet.Status.ReadyReplicas
		}
	}
	result := make([]application.RegionCapacity, 0, len(combined))
	for region, available := range combined {
		result = append(result, application.RegionCapacity{Region: region, AvailableServers: available})
	}
	return result, nil
}

func (a *Allocator) Allocate(ctx context.Context, matchID, region string) (application.Allocation, error) {
	matchID = strings.TrimSpace(matchID)
	region = strings.ToLower(strings.TrimSpace(region))
	if matchID == "" || region == "" {
		return application.Allocation{}, ErrInvalidConfig
	}
	token, err := a.tokenGenerator()
	if err != nil {
		return application.Allocation{}, err
	}
	request := allocationResource{
		APIVersion: "allocation.agones.dev/v1",
		Kind:       "GameServerAllocation",
		Metadata:   allocationMetadata{GenerateName: "matchmind-"},
		Spec: allocationSpec{
			Selectors: []allocationSelector{{
				MatchLabels:     map[string]string{a.gameLabelKey: a.gameLabelValue, a.regionLabelKey: region},
				GameServerState: "Ready",
			}},
			Metadata: allocationMutation{Annotations: map[string]string{
				"matchmind.dev/match-id": matchID, "matchmind.dev/connection-token": token,
			}},
		},
	}
	endpoint := fmt.Sprintf("%s/apis/allocation.agones.dev/v1/namespaces/%s/gameserverallocations", a.apiURL, url.PathEscape(a.namespace))
	var response allocationResource
	if err := a.doJSON(ctx, http.MethodPost, endpoint, request, &response); err != nil {
		return application.Allocation{}, err
	}
	if !strings.EqualFold(response.Status.State, "Allocated") {
		return application.Allocation{}, ErrAllocationRejected
	}
	if strings.TrimSpace(response.Status.Address) == "" || len(response.Status.Ports) == 0 || response.Status.Ports[0].Port <= 0 {
		return application.Allocation{}, ErrInvalidResponse
	}
	port := response.Status.Ports[0].Port
	for _, candidate := range response.Status.Ports {
		if candidate.Name == "default" && candidate.Port > 0 {
			port = candidate.Port
			break
		}
	}
	return application.Allocation{
		Address: net.JoinHostPort(strings.TrimSpace(response.Status.Address), fmt.Sprintf("%d", port)),
		Token:   token,
	}, nil
}

func (a *Allocator) doJSON(ctx context.Context, method, endpoint string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if a.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+a.bearerToken)
	}
	response, err := a.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Agones API %s: %s: %s", method, response.Status, strings.TrimSpace(string(message)))
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, responseLimit))
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode Agones response: %w", err)
	}
	return nil
}

type fleetList struct {
	Items []struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Status struct {
			ReadyReplicas int `json:"readyReplicas"`
		} `json:"status"`
	} `json:"items"`
}

type allocationResource struct {
	APIVersion string             `json:"apiVersion,omitempty"`
	Kind       string             `json:"kind,omitempty"`
	Metadata   allocationMetadata `json:"metadata,omitempty"`
	Spec       allocationSpec     `json:"spec,omitempty"`
	Status     allocationStatus   `json:"status,omitempty"`
}

type allocationMetadata struct {
	GenerateName string `json:"generateName,omitempty"`
}

type allocationSpec struct {
	Selectors []allocationSelector `json:"selectors"`
	Metadata  allocationMutation   `json:"metadata,omitempty"`
}

type allocationSelector struct {
	MatchLabels     map[string]string `json:"matchLabels"`
	GameServerState string            `json:"gameServerState,omitempty"`
}

type allocationMutation struct {
	Annotations map[string]string `json:"annotations,omitempty"`
}

type allocationStatus struct {
	State   string `json:"state"`
	Address string `json:"address"`
	Ports   []struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	} `json:"ports"`
}
