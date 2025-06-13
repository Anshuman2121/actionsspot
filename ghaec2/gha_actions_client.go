package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
)

// GitHub Actions Service API endpoints - using correct endpoints from actions-runner-controller
const (
	scaleSetEndpoint = "_apis/runtime/runnerscalesets"
	apiVersion       = "6.0-preview"
)

// AcquirableJob represents a job that can be acquired by a runner
type AcquirableJob struct {
	AcquireJobURL   string   `json:"acquireJobUrl"`
	MessageType     string   `json:"messageType"`
	RunnerRequestID int64    `json:"runnerRequestId"`
	RepositoryName  string   `json:"repositoryName"`
	OwnerName       string   `json:"ownerName"`
	JobWorkflowRef  string   `json:"jobWorkflowRef"`
	EventName       string   `json:"eventName"`
	RequestLabels   []string `json:"requestLabels"`
}

// AcquirableJobList represents the response from the acquirable jobs API
type AcquirableJobList struct {
	Count int             `json:"count"`
	Jobs  []AcquirableJob `json:"value"`
}

// RunnerScaleSetSession represents a session for message polling
type RunnerScaleSetSession struct {
	SessionID               *uuid.UUID               `json:"sessionId,omitempty"`
	OwnerName               string                   `json:"ownerName,omitempty"`
	RunnerScaleSet          *RunnerScaleSet          `json:"runnerScaleSet,omitempty"`
	MessageQueueURL         string                   `json:"messageQueueUrl,omitempty"`
	MessageQueueAccessToken string                   `json:"messageQueueAccessToken,omitempty"`
	Statistics              *RunnerScaleSetStatistic `json:"statistics,omitempty"`
}

// RunnerScaleSet represents a GitHub Actions runner scale set
type RunnerScaleSet struct {
	ID            int           `json:"id"`
	Name          string        `json:"name"`
	RunnerGroupID int           `json:"runnerGroupId"`
	Labels        []Label       `json:"labels"`
	RunnerSetting RunnerSetting `json:"runnerSetting"`
}

// Label represents a runner label
type Label struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// RunnerSetting represents runner configuration
type RunnerSetting struct {
	Ephemeral     bool `json:"ephemeral"`
	IsElastic     bool `json:"isElastic"`
	DisableUpdate bool `json:"disableUpdate"`
}

// RunnerScaleSetStatistic represents runtime statistics
type RunnerScaleSetStatistic struct {
	TotalAvailableJobs     int `json:"totalAvailableJobs"`
	TotalAcquiredJobs      int `json:"totalAcquiredJobs"`
	TotalAssignedJobs      int `json:"totalAssignedJobs"`
	TotalRunningJobs       int `json:"totalRunningJobs"`
	TotalRegisteredRunners int `json:"totalRegisteredRunners"`
	TotalBusyRunners       int `json:"totalBusyRunners"`
	TotalIdleRunners       int `json:"totalIdleRunners"`
}

// RunnerScaleSetMessage represents a message from the Actions Service
type RunnerScaleSetMessage struct {
	MessageID   int64                    `json:"messageId"`
	MessageType string                   `json:"messageType"`
	Body        string                   `json:"body"`
	Statistics  *RunnerScaleSetStatistic `json:"statistics,omitempty"`
}

// JobAvailable represents a job available message
type JobAvailable struct {
	MessageType     string   `json:"messageType"`
	RunnerRequestID int64    `json:"runnerRequestId"`
	RepositoryName  string   `json:"repositoryName"`
	OwnerName       string   `json:"ownerName"`
	JobWorkflowRef  string   `json:"jobWorkflowRef"`
	EventName       string   `json:"eventName"`
	RequestLabels   []string `json:"requestLabels"`
}

// JobMessageBase represents a base job message
type JobMessageBase struct {
	MessageType        string    `json:"messageType"`
	RunnerRequestID    int64     `json:"runnerRequestId"`
	RepositoryName     string    `json:"repositoryName"`
	OwnerName          string    `json:"ownerName"`
	JobWorkflowRef     string    `json:"jobWorkflowRef"`
	JobDisplayName     string    `json:"jobDisplayName"`
	WorkflowRunID      int64     `json:"workflowRunId"`
	EventName          string    `json:"eventName"`
	RequestLabels      []string  `json:"requestLabels"`
	QueueTime          time.Time `json:"queueTime"`
	ScaleSetAssignTime time.Time `json:"scaleSetAssignTime"`
	RunnerAssignTime   time.Time `json:"runnerAssignTime"`
	FinishTime         time.Time `json:"finishTime"`
}

// ActionsError represents an error from the Actions service
type ActionsError struct {
	StatusCode int
	ActivityID string
	Message    string
	Err        error
}

func (e *ActionsError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("Actions API error (status: %d, activity: %s): %v", e.StatusCode, e.ActivityID, e.Err)
	}
	return fmt.Sprintf("Actions API error (status: %d, activity: %s): %s", e.StatusCode, e.ActivityID, e.Message)
}

// registrationToken represents the GitHub registration token response
type registrationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ActionsServiceAdminConnection represents the response from admin connection endpoint
type ActionsServiceAdminConnection struct {
	ActionsServiceURL *string `json:"url,omitempty"`
	AdminToken        *string `json:"token,omitempty"`
}

// ActionsServiceClient provides access to GitHub Actions Service APIs
type ActionsServiceClient struct {
	httpClient        *http.Client
	baseURL           string
	token             string
	logger            logr.Logger
	actionsServiceURL string
	adminToken        string
	adminTokenExpiry  time.Time
	config            *GitHubConfig
}

// GitHubConfig represents the parsed GitHub configuration URL
type GitHubConfig struct {
	ConfigURL    *url.URL
	Scope        GitHubScope
	Organization string
	Enterprise   string
	Repository   string
}

type GitHubScope string

const (
	GitHubScopeOrganization GitHubScope = "organization"
	GitHubScopeEnterprise   GitHubScope = "enterprise"
	GitHubScopeRepository   GitHubScope = "repository"
)

// GitHubAPIURL constructs the GitHub API URL for a given path
func (g *GitHubConfig) GitHubAPIURL(path string) *url.URL {
	u := *g.ConfigURL
	// Reset path to just the host, then add API path
	u.Path = "/api/v3" + path
	return &u
}

// ParseGitHubConfigFromURL parses a GitHub configuration URL
func ParseGitHubConfigFromURL(githubConfigURL string) (*GitHubConfig, error) {
	u, err := url.Parse(githubConfigURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse github config url: %w", err)
	}

	config := &GitHubConfig{
		ConfigURL: u,
	}

	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")

	if len(pathParts) >= 1 {
		config.Organization = pathParts[0]
		config.Scope = GitHubScopeOrganization
	}

	if len(pathParts) >= 2 {
		config.Repository = pathParts[1]
		config.Scope = GitHubScopeRepository
	}

	return config, nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// NewActionsServiceClient creates a new Actions Service client
func NewActionsServiceClient(gitHubEnterpriseURL, token string, logger logr.Logger) *ActionsServiceClient {
	baseURL := strings.TrimSuffix(gitHubEnterpriseURL, "/")

	return &ActionsServiceClient{
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // timeout must be > 1m to accommodate long polling (like official implementation)
		},
		baseURL: baseURL,
		token:   token,
		logger:  logger,
	}
}

// InitializeConfig initializes the GitHub config for the given organization
func (c *ActionsServiceClient) InitializeConfig(org string) error {
	// Construct the GitHub config URL for the organization
	configURL := fmt.Sprintf("%s/%s", c.baseURL, org)

	config, err := ParseGitHubConfigFromURL(configURL)
	if err != nil {
		c.logger.Error(err, "Failed to parse GitHub config URL")
		return fmt.Errorf("failed to parse GitHub config URL: %w", err)
	}

	c.config = config
	c.logger.Info("GitHub config initialized", "configURL", config.ConfigURL.String())
	return nil
}

// Initialize discovers the Actions Service URL and gets admin token
func (c *ActionsServiceClient) Initialize(ctx context.Context, org string) error {
	c.logger.Info("Initializing Actions Service client", "organization", org)

	// Initialize the GitHub config for this organization
	if err := c.InitializeConfig(org); err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}

	// First, verify the token is valid and has proper permissions
	if err := c.verifyToken(ctx, org); err != nil {
		return fmt.Errorf("token verification failed: %w", err)
	}

	// Check GHES version compatibility
	if err := c.checkGHESCompatibility(ctx); err != nil {
		return err
	}

	// First, try to get a registration token to discover the Actions Service URL
	regToken, err := c.getRegistrationToken(ctx, org)
	if err != nil {
		return fmt.Errorf("failed to get registration token: %w", err)
	}

	c.logger.Info("Successfully obtained registration token")

	// Get Actions Service admin connection
	adminConn, err := c.getActionsServiceAdminConnection(ctx, regToken, org)
	if err != nil {
		return fmt.Errorf("failed to get Actions Service admin connection: %w", err)
	}

	if adminConn.ActionsServiceURL == nil || adminConn.AdminToken == nil {
		return fmt.Errorf("invalid Actions Service connection response - missing URL or token")
	}

	c.actionsServiceURL = *adminConn.ActionsServiceURL
	c.adminToken = *adminConn.AdminToken
	c.adminTokenExpiry = time.Now().Add(1 * time.Hour) // Tokens typically expire in 1 hour

	c.logger.Info("Successfully initialized Actions Service client",
		"actionsServiceURL", c.actionsServiceURL,
		"tokenExpiry", c.adminTokenExpiry,
	)

	return nil
}

// getRegistrationToken gets a registration token from GitHub
func (c *ActionsServiceClient) getRegistrationToken(ctx context.Context, org string) (*registrationToken, error) {
	path := fmt.Sprintf("/orgs/%s/actions/runners/registration-token", org)

	req, err := c.NewGitHubAPIRequest(ctx, "POST", path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.logger.Info("Registration token request", "url", req.URL.String())

	// Set authentication headers after creating request (simplified like official implementation)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	req.Header.Set("Content-Type", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.parseErrorResponse(resp)
	}

	// Debug: Read the response body first to see what we're getting
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	c.logger.Info("Registration token response", "statusCode", resp.StatusCode, "contentType", resp.Header.Get("Content-Type"), "bodyLength", len(bodyBytes))

	// Check if response is HTML (which would indicate an error)
	bodyStr := string(bodyBytes)
	if strings.Contains(bodyStr, "<html") || strings.Contains(bodyStr, "<!DOCTYPE") {
		return nil, fmt.Errorf("registration token endpoint returned HTML instead of JSON. This indicates your GHES version may not support this API endpoint. Response: %s", bodyStr[:min(500, len(bodyStr))])
	}

	var token registrationToken
	if err := json.Unmarshal(bodyBytes, &token); err != nil {
		return nil, fmt.Errorf("failed to decode registration token (body: %s): %w", bodyStr[:min(200, len(bodyStr))], err)
	}

	return &token, nil
}

// NewGitHubAPIRequest creates a new GitHub API request (matching official controller pattern)
func (c *ActionsServiceClient) NewGitHubAPIRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	u := c.config.GitHubAPIURL(path)

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("failed to create new GitHub API request: %w", err)
	}

	req.Header.Set("User-Agent", "ghaec2-scaler/1.0")

	return req, nil
}

// getActionsServiceAdminConnection gets the Actions Service URL and admin token
func (c *ActionsServiceClient) getActionsServiceAdminConnection(ctx context.Context, regToken *registrationToken, org string) (*ActionsServiceAdminConnection, error) {
	path := "/actions/runner-registration"

	// Create request body exactly like the official controller
	body := struct {
		Url         string `json:"url"`
		RunnerEvent string `json:"runner_event"`
	}{
		Url:         c.config.ConfigURL.String(), // Use proper config URL like official implementation
		RunnerEvent: "register",
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(body); err != nil {
		return nil, fmt.Errorf("failed to encode body: %w", err)
	}

	// Use NewGitHubAPIRequest to match official controller pattern
	req, err := c.NewGitHubAPIRequest(ctx, http.MethodPost, path, buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create new GitHub API request: %w", err)
	}

	// Override Authorization header for RemoteAuth
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("RemoteAuth %s", regToken.Token))

	c.logger.Info("Getting Actions Service admin connection (trying Actions Service API)",
		"registrationURL", req.URL.String(),
		"regTokenLength", len(regToken.Token))

	// Implement retry logic exactly like official controller
	var resp *http.Response
	retry := 0
	for {
		var err error
		resp, err = c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to issue the request: %w", err)
		}
		defer resp.Body.Close()

		// Success case
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			break
		}

		// Read response body for error analysis
		body, readErr := io.ReadAll(resp.Body)
		var innerErr error
		if readErr != nil {
			innerErr = readErr
		} else {
			innerErr = fmt.Errorf("%s", string(body))
		}

		// Check if this is an HTML response (indication GHES doesn't support Actions Service)
		if resp.StatusCode == 404 || strings.Contains(string(body), "<html") || strings.Contains(string(body), "<!DOCTYPE") {
			return nil, fmt.Errorf("Actions Service API not supported on this GitHub Enterprise Server version. " +
				"The endpoint '/actions/runner-registration' returned HTML instead of JSON. " +
				"Please upgrade to a GHES version that supports Actions Service API (3.5+) or use traditional runners")
		}

		// Handle auth errors with retry (like official controller)
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			return nil, fmt.Errorf("Actions Service registration failed (status: %d): %w", resp.StatusCode, innerErr)
		}

		retry++
		if retry > 5 {
			return nil, fmt.Errorf("unable to register with Actions Service after 5 retries: %w", innerErr)
		}

		// Add exponential backoff + jitter like official controller
		baseDelay := 500 * time.Millisecond
		jitter := time.Duration(rand.Intn(1000))
		maxDelay := 20 * time.Second
		delay := baseDelay*(1<<retry) + jitter*time.Millisecond

		if delay > maxDelay {
			delay = maxDelay
		}

		c.logger.Info("Retrying Actions Service registration", "retry", retry, "delay", delay)
		time.Sleep(delay)
	}

	var actionsServiceAdminConnection *ActionsServiceAdminConnection
	if err := json.NewDecoder(resp.Body).Decode(&actionsServiceAdminConnection); err != nil {
		return nil, fmt.Errorf("failed to decode Actions Service response: %w", err)
	}

	if actionsServiceAdminConnection.ActionsServiceURL == nil || actionsServiceAdminConnection.AdminToken == nil {
		return nil, fmt.Errorf("invalid Actions Service connection response - missing URL or token")
	}

	c.logger.Info("Successfully obtained Actions Service admin connection",
		"actionsServiceURL", *actionsServiceAdminConnection.ActionsServiceURL)

	return actionsServiceAdminConnection, nil
}

// refreshTokenIfNeeded refreshes the admin token if it's close to expiry
func (c *ActionsServiceClient) refreshTokenIfNeeded(ctx context.Context) error {
	if time.Now().Before(c.adminTokenExpiry.Add(-5 * time.Minute)) {
		return nil // Token is still valid
	}

	c.logger.Info("Refreshing admin token")

	// For Actions Service, we need to re-authenticate
	return fmt.Errorf("token refresh not implemented - please reinitialize the client")
}

// GetOrCreateRunnerScaleSet gets or creates a runner scale set
func (c *ActionsServiceClient) GetOrCreateRunnerScaleSet(ctx context.Context, name string, labels []string, runnerGroupID int) (*RunnerScaleSet, error) {
	c.logger.Info("Getting or creating runner scale set", "name", name, "runnerGroupId", runnerGroupID)

	// First, try to list existing scale sets for debugging
	if err := c.listExistingScaleSets(ctx); err != nil {
		c.logger.Error(err, "Failed to list existing scale sets (non-fatal)")
	}

	// Try to get existing scale set first
	existingScaleSet, err := c.findExistingScaleSet(ctx, name, labels)
	if err != nil {
		c.logger.Error(err, "Failed to find existing scale set")
	}
	if existingScaleSet != nil {
		c.logger.Info("Found compatible existing scale set", 
			"id", existingScaleSet.ID, 
			"name", existingScaleSet.Name,
			"labels", c.extractLabelNames(existingScaleSet.Labels))
		return existingScaleSet, nil
	}

	// Create labels array
	labelsArray := make([]map[string]interface{}, len(labels))
	for i, label := range labels {
		labelsArray[i] = map[string]interface{}{
			"name": label,
			"type": "User",
		}
	}

	payload := map[string]interface{}{
		"name":          name,
		"runnerGroupId": runnerGroupID,  // Add runner group ID
		"labels":        labelsArray,
		"runnerSetting": map[string]interface{}{
			"ephemeral":     true,
			"isElastic":     true,
			"disableUpdate": false,
		},
	}

	c.logger.Info("Creating new scale set", "name", name, "labels", labels, "runnerGroupId", runnerGroupID)

	url := fmt.Sprintf("%s%s?api-version=%s", c.actionsServiceURL, scaleSetEndpoint, apiVersion)
	resp, err := c.makeActionsServiceRequest(ctx, http.MethodPost, url, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to create scale set request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body for debugging
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	c.logger.Info("Scale set creation response", 
		"statusCode", resp.StatusCode,
		"body", string(body))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("failed to create scale set (status %d): %s", resp.StatusCode, string(body))
	}

	var scaleSet RunnerScaleSet
	if err := json.Unmarshal(body, &scaleSet); err != nil {
		return nil, fmt.Errorf("failed to decode scale set response: %w", err)
	}

	// Validate the response
	if scaleSet.ID == 0 || scaleSet.Name == "" {
		return nil, fmt.Errorf("invalid scale set response: ID=%d, Name='%s'", scaleSet.ID, scaleSet.Name)
	}

	c.logger.Info("Scale set created successfully", "id", scaleSet.ID, "name", scaleSet.Name)
	return &scaleSet, nil
}

// findExistingScaleSet tries to find an existing scale set that matches name or labels
func (c *ActionsServiceClient) findExistingScaleSet(ctx context.Context, name string, requestedLabels []string) (*RunnerScaleSet, error) {
	url := fmt.Sprintf("%s%s?api-version=%s", c.actionsServiceURL, scaleSetEndpoint, apiVersion)
	resp, err := c.makeActionsServiceRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list scale sets: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse the response
	var response struct {
		Count int               `json:"count"`
		Value []RunnerScaleSet `json:"value"`
	}
	
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse scale sets response: %w", err)
	}

	c.logger.Info("Found existing scale sets", "count", response.Count)
	for i, ss := range response.Value {
		existingLabels := c.extractLabelNames(ss.Labels)
		c.logger.Info("Existing scale set", 
			"index", i, 
			"id", ss.ID, 
			"name", ss.Name,
			"labels", existingLabels)

		// Check if this scale set matches by name
		if ss.Name == name {
			c.logger.Info("Found scale set by name match", "name", name)
			return &ss, nil
		}

		// Check if this scale set has compatible labels
		if c.labelsMatch(existingLabels, requestedLabels) {
			c.logger.Info("Found scale set with compatible labels", 
				"existing", existingLabels, 
				"requested", requestedLabels)
			return &ss, nil
		}
	}

	return nil, nil // No matching scale set found
}

// labelsMatch checks if existing labels are compatible with requested labels
func (c *ActionsServiceClient) labelsMatch(existing, requested []string) bool {
	// For now, require exact match of all requested labels
	// This could be made more flexible later
	
	existingSet := make(map[string]bool)
	for _, label := range existing {
		existingSet[label] = true
	}

	for _, reqLabel := range requested {
		if !existingSet[reqLabel] {
			return false
		}
	}

	return len(requested) > 0 // Only match if there are requested labels
}

// listExistingScaleSets lists existing scale sets for debugging
func (c *ActionsServiceClient) listExistingScaleSets(ctx context.Context) error {
	url := fmt.Sprintf("%s%s?api-version=%s", c.actionsServiceURL, scaleSetEndpoint, apiVersion)
	resp, err := c.makeActionsServiceRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to list scale sets: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	c.logger.Info("Existing scale sets response", "body", string(body))

	// Try to parse as array of scale sets
	var scaleSets []RunnerScaleSet
	if err := json.Unmarshal(body, &scaleSets); err == nil {
		c.logger.Info("Found existing scale sets", "count", len(scaleSets))
		for i, ss := range scaleSets {
			c.logger.Info("Existing scale set", 
				"index", i, 
				"id", ss.ID, 
				"name", ss.Name,
				"labels", c.extractLabelNames(ss.Labels))
		}
	}

	return nil
}

// extractLabelNames extracts label names from Label array
func (c *ActionsServiceClient) extractLabelNames(labels []Label) []string {
	names := make([]string, len(labels))
	for i, label := range labels {
		names[i] = label.Name
	}
	return names
}

// GetAcquirableJobs gets jobs that can be acquired by the scale set
func (c *ActionsServiceClient) GetAcquirableJobs(ctx context.Context, scaleSetID int) (*AcquirableJobList, error) {
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	path := fmt.Sprintf("/%s/%d/acquirablejobs", scaleSetEndpoint, scaleSetID)
	url := fmt.Sprintf("%s%s?api-version=%s", c.actionsServiceURL, path, apiVersion)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.adminToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return &AcquirableJobList{Count: 0, Jobs: []AcquirableJob{}}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var jobList AcquirableJobList
	if err := json.NewDecoder(resp.Body).Decode(&jobList); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &jobList, nil
}

// CreateMessageSession creates a session for receiving real-time messages
func (c *ActionsServiceClient) CreateMessageSession(ctx context.Context, scaleSetID int, owner string) (*RunnerScaleSetSession, error) {
	if err := c.refreshTokenIfNeeded(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	path := fmt.Sprintf("/%s/%d/sessions", scaleSetEndpoint, scaleSetID)
	url := fmt.Sprintf("%s%s?api-version=%s", c.actionsServiceURL, path, apiVersion)

	newSession := &RunnerScaleSetSession{
		OwnerName: owner,
		RunnerScaleSet: &RunnerScaleSet{
			ID:   scaleSetID,
			Name: "ghaec2-scaler",
		},
	}

	body, err := json.Marshal(newSession)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.adminToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var session RunnerScaleSetSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &session, nil
}

// GetMessage polls for new messages from the message queue
func (c *ActionsServiceClient) GetMessage(ctx context.Context, messageQueueURL, accessToken string, lastMessageID int64, maxCapacity int) (*RunnerScaleSetMessage, error) {
	// Parse the existing URL to properly add query parameters
	u, err := url.Parse(messageQueueURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse message queue URL: %w", err)
	}

	// Add lastMessageId parameter only if > 0 (like official implementation)
	if lastMessageID > 0 {
		params := u.Query()
		params.Set("lastMessageId", fmt.Sprintf("%d", lastMessageID))
		u.RawQuery = params.Encode()
	}

	// Validate maxCapacity (like official implementation)
	if maxCapacity < 0 {
		return nil, fmt.Errorf("maxCapacity must be greater than or equal to 0")
	}

	c.logger.V(1).Info("Making message queue request", 
		"url", u.String(), 
		"lastMessageId", lastMessageID, 
		"maxCapacity", maxCapacity)

	// Use GET method like official implementation
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Use exact headers from official implementation
	req.Header.Set("Accept", "application/json; api-version=6.0-preview")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("User-Agent", "ghaec2-scaler/1.0")
	req.Header.Set("X-GitHub-Actions-Scale-Set-Max-Capacity", fmt.Sprintf("%d", maxCapacity))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error(err, "Failed to execute message queue request")
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	c.logger.V(1).Info("Message queue response", 
		"statusCode", resp.StatusCode,
		"contentType", resp.Header.Get("Content-Type"),
		"requestId", resp.Header.Get("X-GitHub-Request-Id"))

	// Handle StatusAccepted like official implementation
	if resp.StatusCode == http.StatusAccepted {
		c.logger.V(1).Info("No messages available (HTTP 202)")
		return nil, nil // No messages
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Error(nil, "Message queue request failed", 
			"statusCode", resp.StatusCode,
			"requestId", resp.Header.Get("X-GitHub-Request-Id"))
		return nil, c.parseErrorResponse(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	c.logger.V(1).Info("Message queue response body", 
		"bodyLength", len(body),
		"body", string(body))

	var message RunnerScaleSetMessage
	if err := json.Unmarshal(body, &message); err != nil {
		c.logger.Error(err, "Failed to unmarshal message", "body", string(body))
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	c.logger.Info("Successfully received message", 
		"messageId", message.MessageID,
		"messageType", message.MessageType,
		"hasStatistics", message.Statistics != nil,
		"bodyLength", len(message.Body))

	return &message, nil
}

// parseErrorResponse parses error responses from the API
func (c *ActionsServiceClient) parseErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ActionsError{
			StatusCode: resp.StatusCode,
			ActivityID: resp.Header.Get("X-GitHub-Request-Id"),
			Message:    "Failed to read error response",
		}
	}

	c.logger.Info("API error response",
		"statusCode", resp.StatusCode,
		"requestId", resp.Header.Get("X-GitHub-Request-Id"),
		"body", string(body))

	// Try to parse as GitHub API error
	var ghErr struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Field   string `json:"field"`
		} `json:"errors"`
		DocumentationURL string `json:"documentation_url"`
	}

	if err := json.Unmarshal(body, &ghErr); err != nil {
		// If we can't parse the JSON, return the raw body
		return &ActionsError{
			StatusCode: resp.StatusCode,
			ActivityID: resp.Header.Get("X-GitHub-Request-Id"),
			Message:    string(body),
		}
	}

	// Build detailed error message
	var messages []string
	messages = append(messages, ghErr.Message)
	for _, e := range ghErr.Errors {
		if e.Message != "" {
			messages = append(messages, fmt.Sprintf("%s: %s", e.Field, e.Message))
		}
	}

	return &ActionsError{
		StatusCode: resp.StatusCode,
		ActivityID: resp.Header.Get("X-GitHub-Request-Id"),
		Message:    strings.Join(messages, "; "),
		Err:        fmt.Errorf("documentation: %s", ghErr.DocumentationURL),
	}
}

// checkGHESCompatibility checks if the GHES version supports Actions Service API
func (c *ActionsServiceClient) checkGHESCompatibility(ctx context.Context) error {
	// Try to get GHES version info
	path := "/api/v3/meta"
	req, err := c.NewGitHubAPIRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		c.logger.Info("Could not create version check request", "error", err)
		return nil // Don't fail on version check
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Info("Could not check GHES version", "error", err)
		return nil // Don't fail on version check
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var meta struct {
			GitHubServicesGheVersion string `json:"github_services_ghe_version"`
			InstalledVersion         string `json:"installed_version"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&meta); err == nil {
			version := meta.GitHubServicesGheVersion
			if version == "" {
				version = meta.InstalledVersion
			}

			c.logger.Info("Detected GitHub Enterprise Server version", "version", version)

			// Actions Service API was introduced in GHES 3.5+
			if strings.HasPrefix(version, "3.0") || strings.HasPrefix(version, "3.1") ||
				strings.HasPrefix(version, "3.2") || strings.HasPrefix(version, "3.3") ||
				strings.HasPrefix(version, "3.4") {
				return fmt.Errorf("GitHub Enterprise Server version %s detected. Actions Service API requires GHES 3.5 or later. "+
					"Please upgrade your GHES instance or use traditional runners", version)
			}
		}
	}

	return nil
}

// verifyToken checks if the GitHub token is valid and has required permissions
func (c *ActionsServiceClient) verifyToken(ctx context.Context, org string) error {
	c.logger.Info("Verifying GitHub token permissions", "organization", org)

	// Test 1: Check if token can access the API at all
	path := "/user"
	req, err := c.NewGitHubAPIRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("failed to create user request: %w", err)
	}

	// Add authentication headers (simplified like official implementation)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	req.Header.Set("Content-Type", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute user request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("GitHub token is invalid or expired. Please check your GITHUB_TOKEN environment variable")
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token verification failed (status: %d): %s", resp.StatusCode, string(body))
	}

	var user struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&user); err == nil {
		c.logger.Info("Token validated successfully", "user", user.Login, "type", user.Type)
	}

	// Test 2: Check if token can access the organization
	path = fmt.Sprintf("/orgs/%s", org)
	req, err = c.NewGitHubAPIRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("failed to create org request: %w", err)
	}

	// Add authentication headers (simplified like official implementation)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	req.Header.Set("Content-Type", "application/vnd.github.v3+json")

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute org request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return fmt.Errorf("organization '%s' not found or token doesn't have access to it", org)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("organization access check failed (status: %d): %s", resp.StatusCode, string(body))
	}

	c.logger.Info("Token has access to organization", "organization", org)

	// Test 3: Check if token has Actions permissions
	path = fmt.Sprintf("/orgs/%s/actions/permissions", org)
	req, err = c.NewGitHubAPIRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("failed to create actions permissions request: %w", err)
	}

	// Add authentication headers (simplified like official implementation)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	req.Header.Set("Content-Type", "application/vnd.github.v3+json")

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute actions permissions request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		return fmt.Errorf("token doesn't have Actions permissions. Please ensure the token has 'actions:read' or 'admin:org' scope")
	}

	if resp.StatusCode == 200 {
		c.logger.Info("Token has Actions permissions")
	} else {
		c.logger.Info("Actions permissions check returned status", "status", resp.StatusCode)
	}

	return nil
}

// AcquireJobs acquires available jobs
func (c *ActionsServiceClient) AcquireJobs(ctx context.Context, runnerScaleSetID int, messageQueueAccessToken string, requestIDs []int64) ([]int64, error) {
	payload := map[string]interface{}{
		"requestIds": requestIDs,
	}

	url := fmt.Sprintf("%s/%s/%d/jobs", c.actionsServiceURL, scaleSetEndpoint, runnerScaleSetID)

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+messageQueueAccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ghaec2-scaler/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to acquire jobs (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Value []int64 `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode acquire jobs response: %w", err)
	}

	return result.Value, nil
}

// RefreshMessageSession refreshes an existing message session
func (c *ActionsServiceClient) RefreshMessageSession(ctx context.Context, runnerScaleSetID int, sessionID *uuid.UUID) (*RunnerScaleSetSession, error) {
	if sessionID == nil {
		return nil, fmt.Errorf("session ID is nil")
	}

	url := fmt.Sprintf("%s/%s/%d/sessions/%s", c.actionsServiceURL, scaleSetEndpoint, runnerScaleSetID, sessionID.String())
	resp, err := c.makeActionsServiceRequest(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh message session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to refresh message session (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var session RunnerScaleSetSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode session response: %w", err)
	}

	return &session, nil
}

// DeleteMessage deletes a processed message
func (c *ActionsServiceClient) DeleteMessage(ctx context.Context, messageQueueURL, messageQueueAccessToken string, messageID int64) error {
	if messageQueueURL == "" || messageID == 0 {
		return nil // Nothing to delete
	}

	// Parse the existing URL to properly add query parameters
	u, err := url.Parse(messageQueueURL)
	if err != nil {
		return fmt.Errorf("failed to parse message queue URL: %w", err)
	}

	// Get existing query parameters and add messageId
	params := u.Query()
	params.Set("messageId", fmt.Sprintf("%d", messageID))

	// Update the URL with the new parameters
	u.RawQuery = params.Encode()
	finalURL := u.String()

	req, err := http.NewRequestWithContext(ctx, "DELETE", finalURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+messageQueueAccessToken)
	req.Header.Set("User-Agent", "ghaec2-scaler/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete message (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// DeleteMessageSession deletes a message session
func (c *ActionsServiceClient) DeleteMessageSession(ctx context.Context, runnerScaleSetID int, sessionID *uuid.UUID) error {
	if sessionID == nil {
		return nil // Nothing to delete
	}

	url := fmt.Sprintf("%s/%s/%d/sessions/%s?api-version=%s", c.actionsServiceURL, scaleSetEndpoint, runnerScaleSetID, sessionID.String(), apiVersion)
	resp, err := c.makeActionsServiceRequest(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to delete message session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete message session (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// makeActionsServiceRequest makes a request to the Actions Service
func (c *ActionsServiceClient) makeActionsServiceRequest(ctx context.Context, method, url string, payload interface{}) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Use admin token for Actions Service requests
	if c.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.adminToken)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ghaec2-scaler/1.0")

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// GetAdminToken returns the admin token for message queue access
func (c *ActionsServiceClient) GetAdminToken() string {
	return c.adminToken
}

// GetActiveSessions lists active sessions for debugging (not part of official API but helpful for troubleshooting)
func (c *ActionsServiceClient) GetActiveSessions(ctx context.Context, scaleSetID int) error {
	c.logger.Info("Attempting to debug active sessions", "scaleSetId", scaleSetID)
	
	// This is a diagnostic attempt - the official API might not expose this endpoint
	// but we can try to gather information for troubleshooting
	
	return nil
}

// ForceDeleteSession attempts to delete a session by ID (for conflict resolution)
func (c *ActionsServiceClient) ForceDeleteSession(ctx context.Context, scaleSetID int, sessionID string) error {
	c.logger.Info("Attempting to force delete session", "scaleSetId", scaleSetID, "sessionId", sessionID)
	
	// Parse session ID as UUID
	sessionUUID, err := uuid.Parse(sessionID)
	if err != nil {
		return fmt.Errorf("invalid session ID format: %w", err)
	}
	
	return c.DeleteMessageSession(ctx, scaleSetID, &sessionUUID)
}
