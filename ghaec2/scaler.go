package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/go-logr/logr"
)

// GHAListenerScaler implements the ghalistener-based scaling approach
type GHAListenerScaler struct {
	config         *Config
	ec2Client      *ec2.Client
	actionsClient  *ActionsServiceClient
	logger         logr.Logger
	
	// Current state
	scaleSet       *RunnerScaleSet
	session        *RunnerScaleSetSession
	lastMessageID  int64
	currentRunners int
}

// NewGHAListenerScaler creates a new scaler instance
func NewGHAListenerScaler(ctx context.Context, config *Config, ec2Client *ec2.Client, logger logr.Logger) (*GHAListenerScaler, error) {
	// Create Actions Service client
	actionsClient := NewActionsServiceClient(config.GitHubEnterpriseURL, config.GitHubToken, logger)
	
	// Initialize the Actions Service client
	if err := actionsClient.Initialize(ctx, config.OrganizationName); err != nil {
		return nil, fmt.Errorf("failed to initialize Actions Service client: %w", err)
	}
	
	scaler := &GHAListenerScaler{
		config:        config,
		ec2Client:     ec2Client,
		actionsClient: actionsClient,
		logger:        logger,
	}
	
	return scaler, nil
}

// Run starts the scaler main loop
func (s *GHAListenerScaler) Run(ctx context.Context) error {
	s.logger.Info("Starting GHA Listener Scaler")
	
	// Initialize scale set
	if err := s.initializeScaleSet(ctx); err != nil {
		return fmt.Errorf("failed to initialize scale set: %w", err)
	}
	
	// Create message session
	if err := s.createMessageSession(ctx); err != nil {
		return fmt.Errorf("failed to create message session: %w", err)
	}
	defer s.cleanupSession(ctx)
	
	// Handle initial statistics
	if s.session.Statistics != nil {
		s.logger.Info("Initial statistics",
			"availableJobs", s.session.Statistics.TotalAvailableJobs,
			"assignedJobs", s.session.Statistics.TotalAssignedJobs,
			"runningJobs", s.session.Statistics.TotalRunningJobs,
			"registeredRunners", s.session.Statistics.TotalRegisteredRunners,
		)
		
		// Scale based on initial statistics
		if err := s.scaleBasedOnStatistics(ctx, s.session.Statistics); err != nil {
			s.logger.Error(err, "Failed to scale based on initial statistics")
		}
	}
	
	// Start message polling loop
	return s.messagePollingLoop(ctx)
}

// initializeScaleSet creates or gets the runner scale set
func (s *GHAListenerScaler) initializeScaleSet(ctx context.Context) error {
	s.logger.Info("Initializing runner scale set", "name", s.config.RunnerScaleSetName)
	
	scaleSet, err := s.actionsClient.GetOrCreateRunnerScaleSet(ctx, s.config.RunnerScaleSetName, s.config.RunnerLabels)
	if err != nil {
		return fmt.Errorf("failed to get or create scale set: %w", err)
	}
	
	s.scaleSet = scaleSet
	s.config.RunnerScaleSetID = scaleSet.ID
	
	s.logger.Info("Scale set initialized",
		"id", scaleSet.ID,
		"name", scaleSet.Name,
		"labels", s.extractLabelNames(scaleSet.Labels),
	)
	
	return nil
}

// createMessageSession creates a session for real-time message polling
func (s *GHAListenerScaler) createMessageSession(ctx context.Context) error {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "ghaec2-scaler"
	}
	
	s.logger.Info("Creating message session", "owner", hostname)
	
	session, err := s.actionsClient.CreateMessageSession(ctx, s.config.RunnerScaleSetID, hostname)
	if err != nil {
		return fmt.Errorf("failed to create message session: %w", err)
	}
	
	s.session = session
	s.lastMessageID = 0
	
	s.logger.Info("Message session created",
		"sessionId", session.SessionID,
		"messageQueueUrl", session.MessageQueueURL,
	)
	
	return nil
}

// messagePollingLoop continuously polls for messages
func (s *GHAListenerScaler) messagePollingLoop(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second) // Poll every 2 seconds for real-time response
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.pollAndProcessMessages(ctx); err != nil {
				s.logger.Error(err, "Failed to poll and process messages")
				// Continue running despite errors
			}
		}
	}
}

// pollAndProcessMessages polls for new messages and processes them
func (s *GHAListenerScaler) pollAndProcessMessages(ctx context.Context) error {
	message, err := s.actionsClient.GetMessage(ctx, 
		s.session.MessageQueueURL, 
		s.session.MessageQueueAccessToken, 
		s.lastMessageID, 
		s.config.MaxRunners)
	
	if err != nil {
		return fmt.Errorf("failed to get message: %w", err)
	}
	
	if message == nil {
		// No new messages
		return nil
	}
	
	s.lastMessageID = message.MessageID
	
	s.logger.Info("Received message",
		"messageId", message.MessageID,
		"messageType", message.MessageType,
	)
	
	// Update statistics if available
	if message.Statistics != nil {
		if err := s.scaleBasedOnStatistics(ctx, message.Statistics); err != nil {
			s.logger.Error(err, "Failed to scale based on message statistics")
		}
	}
	
	// Process message body if it contains job information
	if message.Body != "" {
		if err := s.processMessageBody(ctx, message); err != nil {
			s.logger.Error(err, "Failed to process message body")
		}
	}
	
	return nil
}

// scaleBasedOnStatistics scales runners based on current statistics
func (s *GHAListenerScaler) scaleBasedOnStatistics(ctx context.Context, stats *RunnerScaleSetStatistic) error {
	s.logger.Info("Processing statistics",
		"availableJobs", stats.TotalAvailableJobs,
		"assignedJobs", stats.TotalAssignedJobs,
		"runningJobs", stats.TotalRunningJobs,
		"registeredRunners", stats.TotalRegisteredRunners,
		"busyRunners", stats.TotalBusyRunners,
		"idleRunners", stats.TotalIdleRunners,
	)
	
	// In fallback mode, also check for acquirable jobs directly
	additionalJobs := 0
	if strings.Contains(s.actionsClient.actionsTokenURL, s.actionsClient.baseURL) && 
	   s.actionsClient.adminToken == s.actionsClient.token {
		s.logger.Info("Fallback mode: checking for acquirable jobs directly")
		
		jobList, err := s.actionsClient.GetAcquirableJobs(ctx, s.config.RunnerScaleSetID)
		if err != nil {
			s.logger.Error(err, "Failed to get acquirable jobs in fallback mode")
		} else {
			additionalJobs = jobList.Count
			s.logger.Info("Found acquirable jobs via fallback", "jobCount", additionalJobs)
			
			// Process each job to trigger scaling
			for _, job := range jobList.Jobs {
				s.logger.Info("Found pending job",
					"repository", job.RepositoryName,
					"owner", job.OwnerName,
					"workflowRef", job.JobWorkflowRef,
					"labels", job.RequestLabels,
				)
			}
		}
	}
	
	// Calculate required runners based on pending jobs (including fallback jobs)
	pendingJobs := stats.TotalAvailableJobs + stats.TotalAssignedJobs + additionalJobs
	
	// Calculate desired runner count
	desiredRunners := pendingJobs
	
	// Apply min/max constraints
	if desiredRunners < s.config.MinRunners {
		desiredRunners = s.config.MinRunners
	}
	if desiredRunners > s.config.MaxRunners {
		desiredRunners = s.config.MaxRunners
	}
	
	// Get current runner count from AWS
	currentRunners, err := s.getCurrentRunnerCount(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current runner count: %w", err)
	}
	
	s.currentRunners = currentRunners
	
	s.logger.Info("Scaling decision",
		"pendingJobs", pendingJobs,
		"additionalJobs", additionalJobs,
		"currentRunners", currentRunners,
		"desiredRunners", desiredRunners,
		"minRunners", s.config.MinRunners,
		"maxRunners", s.config.MaxRunners,
	)
	
	// Scale up if needed
	if desiredRunners > currentRunners {
		runnersToCreate := desiredRunners - currentRunners
		s.logger.Info("Scaling up", "runnersToCreate", runnersToCreate)
		
		for i := 0; i < runnersToCreate; i++ {
			if err := s.createRunner(ctx); err != nil {
				s.logger.Error(err, "Failed to create runner", "attempt", i+1)
				// Continue creating other runners
			}
		}
	}
	
	// Scale down if needed (but be conservative to avoid thrashing)
	if desiredRunners < currentRunners && stats.TotalIdleRunners > 0 {
		runnersToTerminate := currentRunners - desiredRunners
		if runnersToTerminate > stats.TotalIdleRunners {
			runnersToTerminate = stats.TotalIdleRunners
		}
		
		s.logger.Info("Scaling down", "runnersToTerminate", runnersToTerminate)
		
		if err := s.terminateIdleRunners(ctx, runnersToTerminate); err != nil {
			s.logger.Error(err, "Failed to terminate idle runners")
		}
	}
	
	return nil
}

// processMessageBody processes the message body for job-specific events
func (s *GHAListenerScaler) processMessageBody(ctx context.Context, message *RunnerScaleSetMessage) error {
	// Try to parse as JobAvailable message
	var jobAvailable JobAvailable
	if err := json.Unmarshal([]byte(message.Body), &jobAvailable); err == nil {
		if jobAvailable.MessageType == "JobAvailable" {
			return s.handleJobAvailable(ctx, &jobAvailable)
		}
	}
	
	// Try to parse as other job message types
	var jobMessage JobMessageBase
	if err := json.Unmarshal([]byte(message.Body), &jobMessage); err == nil {
		return s.handleJobMessage(ctx, &jobMessage)
	}
	
	s.logger.Info("Unknown message body format", "body", message.Body)
	return nil
}

// handleJobAvailable handles a job available event
func (s *GHAListenerScaler) handleJobAvailable(ctx context.Context, job *JobAvailable) error {
	s.logger.Info("Job available",
		"repository", job.RepositoryName,
		"owner", job.OwnerName,
		"workflowRef", job.JobWorkflowRef,
		"labels", job.RequestLabels,
		"event", job.EventName,
	)
	
	// Check if this job's labels match our runner labels
	if !s.labelsMatch(job.RequestLabels, s.config.RunnerLabels) {
		s.logger.Info("Job labels don't match runner labels, skipping",
			"jobLabels", job.RequestLabels,
			"runnerLabels", s.config.RunnerLabels,
		)
		return nil
	}
	
	s.logger.Info("Job labels match! Creating runner for this job")
	
	// Ensure we have at least one runner available for this job
	currentRunners, err := s.getCurrentRunnerCount(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current runner count: %w", err)
	}
	
	if currentRunners < s.config.MaxRunners {
		s.logger.Info("Creating runner for job", "currentRunners", currentRunners)
		return s.createRunner(ctx)
	}
	
	s.logger.Info("Max runners reached, cannot create more", "maxRunners", s.config.MaxRunners)
	return nil
}

// handleJobMessage handles other job messages (started, completed, etc.)
func (s *GHAListenerScaler) handleJobMessage(ctx context.Context, job *JobMessageBase) error {
	s.logger.Info("Job message",
		"messageType", job.MessageType,
		"repository", job.RepositoryName,
		"workflowRef", job.JobWorkflowRef,
	)
	
	// For job completion, we might want to clean up runners
	if job.MessageType == "JobCompleted" {
		// Let the statistics-based scaling handle cleanup
		s.logger.Info("Job completed, will be handled by statistics-based scaling")
	}
	
	return nil
}

// labelsMatch checks if all job labels are present in runner labels
func (s *GHAListenerScaler) labelsMatch(jobLabels, runnerLabels []string) bool {
	for _, jobLabel := range jobLabels {
		if !slices.Contains(runnerLabels, jobLabel) {
			return false
		}
	}
	return true
}

// getCurrentRunnerCount gets the current number of running EC2 instances
func (s *GHAListenerScaler) getCurrentRunnerCount(ctx context.Context) (int, error) {
	// Describe instances with our specific tags
	result, err := s.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("tag:Type"),
				Values: []string{"ghaec2-runner"},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"pending", "running"},
			},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to describe instances: %w", err)
	}
	
	count := 0
	for _, reservation := range result.Reservations {
		count += len(reservation.Instances)
	}
	
	return count, nil
}

// createRunner creates a new EC2 spot instance
func (s *GHAListenerScaler) createRunner(ctx context.Context) error {
	s.logger.Info("Creating new runner instance")
	
	// Parse spot price
	spotPrice, err := strconv.ParseFloat(s.config.EC2SpotPrice, 64)
	if err != nil {
		return fmt.Errorf("invalid spot price: %w", err)
	}
	
	// Generate unique runner name
	runnerName := fmt.Sprintf("ghaec2-runner-%d", time.Now().Unix())
	
	// Create user data script
	userData := s.generateUserDataScript(runnerName)
	encodedUserData := base64.StdEncoding.EncodeToString([]byte(userData))
	
	// Create spot instance request
	spotRequest := &ec2.RequestSpotInstancesInput{
		SpotPrice:     aws.String(fmt.Sprintf("%.3f", spotPrice)),
		InstanceCount: aws.Int32(1),
		LaunchSpecification: &types.RequestSpotLaunchSpecification{
			ImageId:      aws.String(s.config.EC2AMI),
			InstanceType: types.InstanceType(s.config.EC2InstanceType),
			KeyName:      aws.String(s.config.EC2KeyPairName),
			SecurityGroupIds: []string{
				s.config.EC2SecurityGroupID,
			},
			SubnetId: aws.String(s.config.EC2SubnetID),
			UserData: aws.String(encodedUserData),
		},
	}
	
	result, err := s.ec2Client.RequestSpotInstances(ctx, spotRequest)
	if err != nil {
		return fmt.Errorf("failed to request spot instance: %w", err)
	}
	
	if len(result.SpotInstanceRequests) == 0 {
		return fmt.Errorf("no spot instance requests created")
	}
	
	spotRequestID := *result.SpotInstanceRequests[0].SpotInstanceRequestId
	s.logger.Info("Spot instance requested", 
		"spotRequestId", spotRequestID,
		"runnerName", runnerName,
	)
	
	// Tag the spot request
	_, err = s.ec2Client.CreateTags(ctx, &ec2.CreateTagsInput{
		Resources: []string{spotRequestID},
		Tags: []types.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(runnerName),
			},
			{
				Key:   aws.String("Type"),
				Value: aws.String("ghaec2-runner"),
			},
			{
				Key:   aws.String("ScaleSet"),
				Value: aws.String(s.config.RunnerScaleSetName),
			},
		},
	})
	if err != nil {
		s.logger.Error(err, "Failed to tag spot request", "spotRequestId", spotRequestID)
	}
	
	return nil
}

// generateUserDataScript generates the user data script for runner instances
func (s *GHAListenerScaler) generateUserDataScript(runnerName string) string {
	// Get registration token - this will need to be implemented
	// For now, using placeholder that will be replaced with actual token
	runnerLabelsStr := fmt.Sprintf("%s", strings.Join(s.config.RunnerLabels, ","))
	
	script := fmt.Sprintf(`#!/bin/bash
cd /actions-runner

# Get registration token
REGISTRATION_TOKEN=$(curl -s -X POST \
  -H "Authorization: token %s" \
  -H "Accept: application/vnd.github.v3+json" \
  %s/api/v3/orgs/%s/actions/runners/registration-token | jq -r .token)

# Set up the runner
RUNNER_ALLOW_RUNASROOT=1 ./config.sh --url %s/%s --token $REGISTRATION_TOKEN --name %s --labels %s --ephemeral --runnergroup SpotInstances --work _work --replace

# Install the runner as a service
./svc.sh install

# Start the runner
./svc.sh start

echo "Runner %s started successfully"
`, 
		s.config.GitHubToken,
		s.config.GitHubEnterpriseURL,
		s.config.OrganizationName,
		s.config.GitHubEnterpriseURL,
		s.config.OrganizationName,
		runnerName,
		runnerLabelsStr,
		runnerName,
	)
	
	return script
}

// terminateIdleRunners terminates idle runner instances
func (s *GHAListenerScaler) terminateIdleRunners(ctx context.Context, count int) error {
	s.logger.Info("Terminating idle runners", "count", count)
	
	// Get running instances
	result, err := s.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("tag:Type"),
				Values: []string{"ghaec2-runner"},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running"},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to describe instances: %w", err)
	}
	
	// Collect instance IDs (terminate oldest first)
	var instanceIDs []string
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if len(instanceIDs) < count {
				instanceIDs = append(instanceIDs, *instance.InstanceId)
			}
		}
	}
	
	if len(instanceIDs) == 0 {
		s.logger.Info("No instances to terminate")
		return nil
	}
	
	// Terminate instances
	_, err = s.ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: instanceIDs,
	})
	if err != nil {
		return fmt.Errorf("failed to terminate instances: %w", err)
	}
	
	s.logger.Info("Terminated instances", "instanceIds", instanceIDs)
	return nil
}

// cleanupSession cleans up the message session
func (s *GHAListenerScaler) cleanupSession(ctx context.Context) {
	if s.session != nil && s.session.SessionID != nil {
		s.logger.Info("Cleaning up message session", "sessionId", s.session.SessionID)
		// Implementation would call DeleteMessageSession API
	}
}

// extractLabelNames extracts label names from Label objects
func (s *GHAListenerScaler) extractLabelNames(labels []Label) []string {
	names := make([]string, len(labels))
	for i, label := range labels {
		names[i] = label.Name
	}
	return names
} 