package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const (
	githubAPIURL         = "https://api.github.com"
	scaleSetEndpoint     = "_apis/runtime/runnerscalesets"
	apiVersionQueryParam = "api-version=6.0-preview"
)

type GitHubActionsClientImpl struct {
	config     Config
	httpClient *http.Client
	baseURL    string
}

// NewGitHubActionsClient creates a new GitHub Actions client
func NewGitHubActionsClient(config Config) *GitHubActionsClientImpl {
	return &GitHubActionsClientImpl{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    githubAPIURL,
	}
}

// Generate JWT token for GitHub App authentication
func (c *GitHubActionsClientImpl) generateJWT() (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": c.config.GitHubApp.AppID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	
	// Parse private key
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(c.config.GitHubApp.PrivateKey))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	// Sign token
	tokenString, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return tokenString, nil
}

// Get installation access token
func (c *GitHubActionsClientImpl) getInstallationToken(ctx context.Context) (string, error) {
	jwt, err := c.generateJWT()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.baseURL, c.config.GitHubApp.InstallationID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get installation token: %s", string(body))
	}

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}

	return tokenResp.Token, nil
}

// Make authenticated request to GitHub Actions API
func (c *GitHubActionsClientImpl) makeRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	token, err := c.getInstallationToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// GetAcquirableJobs retrieves jobs that can be acquired by the scale set
func (c *GitHubActionsClientImpl) GetAcquirableJobs(ctx context.Context, runnerScaleSetId int) (*AcquirableJobList, error) {
	path := fmt.Sprintf("/%s/%d/acquirablejobs?%s", scaleSetEndpoint, runnerScaleSetId, apiVersionQueryParam)
	
	resp, err := c.makeRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return &AcquirableJobList{Count: 0, Jobs: []AcquirableJob{}}, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get acquirable jobs: %s", string(body))
	}

	var jobList AcquirableJobList
	if err := json.NewDecoder(resp.Body).Decode(&jobList); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &jobList, nil
}

// CreateMessageSession creates a new message session for the runner scale set
func (c *GitHubActionsClientImpl) CreateMessageSession(ctx context.Context, runnerScaleSetId int, owner string) (*RunnerScaleSetSession, error) {
	path := fmt.Sprintf("/%s/%d/sessions?%s", scaleSetEndpoint, runnerScaleSetId, apiVersionQueryParam)

	sessionRequest := map[string]string{
		"ownerName": owner,
	}

	jsonData, err := json.Marshal(sessionRequest)
	if err != nil {
		return nil, err
	}

	resp, err := c.makeRequest(ctx, "POST", path, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create message session: %s", string(body))
	}

	var session RunnerScaleSetSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &session, nil
}

// GetMessage retrieves the next message from the message queue
func (c *GitHubActionsClientImpl) GetMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, lastMessageId int64, maxCapacity int) (*RunnerScaleSetMessage, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", messageQueueUrl, nil)
	if err != nil {
		return nil, err
	}

	// Add query parameters
	q := req.URL.Query()
	if lastMessageId > 0 {
		q.Set("lastMessageId", strconv.FormatInt(lastMessageId, 10))
	}
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", "Bearer "+messageQueueAccessToken)
	req.Header.Set("Accept", "application/json; api-version=6.0-preview")
	req.Header.Set("X-ScaleSetMaxCapacity", strconv.Itoa(maxCapacity))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		return nil, nil // No message available
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get message: %s", string(body))
	}

	var message RunnerScaleSetMessage
	if err := json.NewDecoder(resp.Body).Decode(&message); err != nil {
		return nil, fmt.Errorf("failed to decode message: %w", err)
	}

	return &message, nil
}

// DeleteMessage deletes a processed message from the queue
func (c *GitHubActionsClientImpl) DeleteMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, messageId int64) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", messageQueueUrl, nil)
	if err != nil {
		return err
	}

	// Add message ID to URL
	q := req.URL.Query()
	q.Set("messageId", strconv.FormatInt(messageId, 10))
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", "Bearer "+messageQueueAccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete message: %s", string(body))
	}

	return nil
}

// AcquireJobs acquires the specified jobs for the runner scale set
func (c *GitHubActionsClientImpl) AcquireJobs(ctx context.Context, runnerScaleSetId int, messageQueueAccessToken string, requestIds []int64) ([]int64, error) {
	path := fmt.Sprintf("/%s/%d/acquirejobs?%s", scaleSetEndpoint, runnerScaleSetId, apiVersionQueryParam)

	jsonData, err := json.Marshal(requestIds)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", path, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+messageQueueAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to acquire jobs: %s", string(body))
	}

	var result struct {
		Value []int64 `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Value, nil
}

// RefreshMessageSession refreshes an existing message session
func (c *GitHubActionsClientImpl) RefreshMessageSession(ctx context.Context, runnerScaleSetId int, sessionId string) (*RunnerScaleSetSession, error) {
	path := fmt.Sprintf("/%s/%d/sessions/%s?%s", scaleSetEndpoint, runnerScaleSetId, sessionId, apiVersionQueryParam)

	resp, err := c.makeRequest(ctx, "PATCH", path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to refresh message session: %s", string(body))
	}

	var session RunnerScaleSetSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &session, nil
}

// DeleteMessageSession deletes a message session
func (c *GitHubActionsClientImpl) DeleteMessageSession(ctx context.Context, runnerScaleSetId int, sessionId string) error {
	path := fmt.Sprintf("/%s/%d/sessions/%s?%s", scaleSetEndpoint, runnerScaleSetId, sessionId, apiVersionQueryParam)

	resp, err := c.makeRequest(ctx, "DELETE", path, nil)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete message session: %s", string(body))
	}

	return nil
}

// ParseJobsFromMessage parses available jobs from a message body
func ParseJobsFromMessage(messageBody string) ([]*JobAvailable, error) {
	if messageBody == "" {
		return nil, nil
	}

	var batchedMessages []json.RawMessage
	if err := json.Unmarshal([]byte(messageBody), &batchedMessages); err != nil {
		return nil, fmt.Errorf("failed to unmarshal batched messages: %w", err)
	}

	var jobsAvailable []*JobAvailable
	for _, msg := range batchedMessages {
		var messageType struct {
			MessageType string `json:"messageType"`
		}
		if err := json.Unmarshal(msg, &messageType); err != nil {
			continue
		}

		if messageType.MessageType == "JobAvailable" {
			var jobAvailable JobAvailable
			if err := json.Unmarshal(msg, &jobAvailable); err != nil {
				continue
			}
			jobsAvailable = append(jobsAvailable, &jobAvailable)
		}
	}

	return jobsAvailable, nil
} 