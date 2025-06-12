package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
)

type GitHubActionsClient interface {
	GetAcquirableJobs(ctx context.Context, runnerScaleSetId int) (*AcquirableJobList, error)
	CreateMessageSession(ctx context.Context, runnerScaleSetId int, owner string) (*RunnerScaleSetSession, error)
	GetMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, lastMessageId int64, maxCapacity int) (*RunnerScaleSetMessage, error)
	DeleteMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, messageId int64) error
	AcquireJobs(ctx context.Context, runnerScaleSetId int, messageQueueAccessToken string, requestIds []int64) ([]int64, error)
	RefreshMessageSession(ctx context.Context, runnerScaleSetId int, sessionId string) (*RunnerScaleSetSession, error)
	DeleteMessageSession(ctx context.Context, runnerScaleSetId int, sessionId string) error
}

// GitHub Actions types
type AcquirableJobList struct {
	Count int             `json:"count"`
	Jobs  []AcquirableJob `json:"value"`
}

type AcquirableJob struct {
	AcquireJobUrl   string   `json:"acquireJobUrl"`
	MessageType     string   `json:"messageType"`
	RunnerRequestId int64    `json:"runnerRequestId"`
	RepositoryName  string   `json:"repositoryName"`
	OwnerName       string   `json:"ownerName"`
	JobWorkflowRef  string   `json:"jobWorkflowRef"`
	EventName       string   `json:"eventName"`
	RequestLabels   []string `json:"requestLabels"`
}

type RunnerScaleSetSession struct {
	SessionId               string                   `json:"sessionId"`
	OwnerName               string                   `json:"ownerName"`
	RunnerScaleSet          *RunnerScaleSet          `json:"runnerScaleSet"`
	MessageQueueUrl         string                   `json:"messageQueueUrl"`
	MessageQueueAccessToken string                   `json:"messageQueueAccessToken"`
	Statistics              *RunnerScaleSetStatistic `json:"statistics"`
}

type RunnerScaleSet struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type RunnerScaleSetMessage struct {
	MessageId   int64                    `json:"messageId"`
	MessageType string                   `json:"messageType"`
	Body        string                   `json:"body"`
	Statistics  *RunnerScaleSetStatistic `json:"statistics"`
}

type RunnerScaleSetStatistic struct {
	TotalAvailableJobs     int `json:"totalAvailableJobs"`
	TotalAcquiredJobs      int `json:"totalAcquiredJobs"`
	TotalAssignedJobs      int `json:"totalAssignedJobs"`
	TotalRunningJobs       int `json:"totalRunningJobs"`
	TotalRegisteredRunners int `json:"totalRegisteredRunners"`
	TotalBusyRunners       int `json:"totalBusyRunners"`
	TotalIdleRunners       int `json:"totalIdleRunners"`
}

type JobAvailable struct {
	AcquireJobUrl string `json:"acquireJobUrl"`
	JobMessageBase
}

type JobMessageBase struct {
	MessageType     string    `json:"messageType"`
	RunnerRequestId int64     `json:"runnerRequestId"`
	RepositoryName  string    `json:"repositoryName"`
	OwnerName       string    `json:"ownerName"`
	JobWorkflowRef  string    `json:"jobWorkflowRef"`
	JobDisplayName  string    `json:"jobDisplayName"`
	EventName       string    `json:"eventName"`
	RequestLabels   []string  `json:"requestLabels"`
	QueueTime       time.Time `json:"queueTime"`
}

// Lambda handler configuration
type Config struct {
	GitHubToken              string
	GitHubEnterpriseURL      string
	OrganizationName         string
	MinRunners               int
	MaxRunners               int
	EC2InstanceType          string
	EC2AMI                   string
	EC2SubnetID              string
	EC2SecurityGroupID       string
	EC2KeyPairName           string
	EC2SpotPrice             string
	DynamoDBTableName        string
	RunnerLabels             []string
	CleanupOfflineRunners    bool
}

type GitHubAppConfig struct {
	AppID          int64
	InstallationID int64
	PrivateKey     string
}

// AWS infrastructure
type AWSInfrastructure struct {
	ec2Client       *ec2.Client
	dynamoDBClient  *dynamodb.Client
	eventBridgeClient *eventbridge.Client
	config          Config
}

// DynamoDB schema for tracking runners and sessions
type RunnerRecord struct {
	RunnerID           string    `dynamodbav:"runner_id"`
	InstanceID         string    `dynamodbav:"instance_id"`
	JobRequestID       int64     `dynamodbav:"job_request_id"`
	Status             string    `dynamodbav:"status"` // pending, running, completed, failed
	CreatedAt          time.Time `dynamodbav:"created_at"`
	UpdatedAt          time.Time `dynamodbav:"updated_at"`
	SpotRequestID      string    `dynamodbav:"spot_request_id,omitempty"`
}

type SessionRecord struct {
	SessionID               string    `dynamodbav:"session_id"`
	MessageQueueUrl         string    `dynamodbav:"message_queue_url"`
	MessageQueueAccessToken string    `dynamodbav:"message_queue_access_token"`
	LastMessageID           int64     `dynamodbav:"last_message_id"`
	CreatedAt               time.Time `dynamodbav:"created_at"`
	UpdatedAt               time.Time `dynamodbav:"updated_at"`
}

// Initialize AWS infrastructure
func NewAWSInfrastructure(ctx context.Context, cfg Config) (*AWSInfrastructure, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &AWSInfrastructure{
		ec2Client:       ec2.NewFromConfig(awsCfg),
		dynamoDBClient:  dynamodb.NewFromConfig(awsCfg),
		eventBridgeClient: eventbridge.NewFromConfig(awsCfg),
		config:          cfg,
	}, nil
}

// Load configuration from environment variables
func LoadConfig() (Config, error) {
	minRunners, err := strconv.Atoi(getEnvOrDefault("MIN_RUNNERS", "0"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid MIN_RUNNERS: %w", err)
	}

	maxRunners, err := strconv.Atoi(getEnvOrDefault("MAX_RUNNERS", "10"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid MAX_RUNNERS: %w", err)
	}

	var runnerLabels []string
	if labels := os.Getenv("RUNNER_LABELS"); labels != "" {
		if err := json.Unmarshal([]byte(labels), &runnerLabels); err != nil {
			return Config{}, fmt.Errorf("invalid RUNNER_LABELS JSON: %w", err)
		}
	}

	cleanupOffline, _ := strconv.ParseBool(getEnvOrDefault("CLEANUP_OFFLINE_RUNNERS", "true"))

	return Config{
		GitHubToken:              os.Getenv("GITHUB_TOKEN"),
		GitHubEnterpriseURL:      getEnvOrDefault("GITHUB_ENTERPRISE_URL", "https://TelenorSwedenAB.ghe.com"),
		OrganizationName:         getEnvOrDefault("ORGANIZATION_NAME", "TelenorSweden"),
		MinRunners:               minRunners,
		MaxRunners:               maxRunners,
		EC2InstanceType:          getEnvOrDefault("EC2_INSTANCE_TYPE", "t3.medium"),
		EC2AMI:                   os.Getenv("EC2_AMI_ID"),
		EC2SubnetID:              os.Getenv("EC2_SUBNET_ID"),
		EC2SecurityGroupID:       os.Getenv("EC2_SECURITY_GROUP_ID"),
		EC2KeyPairName:           os.Getenv("EC2_KEY_PAIR_NAME"),
		EC2SpotPrice:             getEnvOrDefault("EC2_SPOT_PRICE", "0.05"),
		DynamoDBTableName:        getEnvOrDefault("DYNAMODB_TABLE_NAME", "github-runners"),
		RunnerLabels:             runnerLabels,
		CleanupOfflineRunners:    cleanupOffline,
	}, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Create Spot Instance for GitHub Runner
func (aws *AWSInfrastructure) CreateSpotInstance(ctx context.Context, jobID int64, labels []string) (*string, error) {
	// Generate user data script for runner installation
	userData := aws.generateUserDataScript(jobID, labels)

	// Spot instance request specification
	spotPrice := aws.config.EC2SpotPrice
	launchSpec := &ec2types.RequestSpotLaunchSpecification{
		ImageId:        aws.String(aws.config.EC2AMI),
		InstanceType:   ec2types.InstanceType(aws.config.EC2InstanceType),
		KeyName:        aws.String(aws.config.EC2KeyPairName),
		SecurityGroups: []ec2types.GroupIdentifier{{GroupId: aws.String(aws.config.EC2SecurityGroupID)}},
		SubnetId:       aws.String(aws.config.EC2SubnetID),
		UserData:       aws.String(userData),
		Monitoring: &ec2types.RunInstancesMonitoringEnabled{
			Enabled: aws.Bool(true),
		},
	}

	// Create spot instance request
	input := &ec2.RequestSpotInstancesInput{
		SpotPrice:           aws.String(spotPrice),
		InstanceCount:       aws.Int32(1),
		Type:                ec2types.SpotInstanceTypeOneTime,
		LaunchSpecification: launchSpec,
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeSpotInstancesRequest,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String(fmt.Sprintf("github-runner-job-%d", jobID))},
					{Key: aws.String("Purpose"), Value: aws.String("github-actions-runner")},
					{Key: aws.String("JobID"), Value: aws.String(strconv.FormatInt(jobID, 10))},
					{Key: aws.String("ManagedBy"), Value: aws.String("github-runner-scaler-lambda")},
				},
			},
		},
	}

	result, err := aws.ec2Client.RequestSpotInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to request spot instance: %w", err)
	}

	if len(result.SpotInstanceRequests) == 0 {
		return nil, fmt.Errorf("no spot instance requests created")
	}

	spotRequestID := result.SpotInstanceRequests[0].SpotInstanceRequestId
	log.Printf("Created spot instance request: %s for job %d", *spotRequestID, jobID)

	// Store runner record in DynamoDB
	if err := aws.storeRunnerRecord(ctx, RunnerRecord{
		RunnerID:      fmt.Sprintf("runner-%d-%d", jobID, time.Now().Unix()),
		JobRequestID:  jobID,
		Status:        "pending",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		SpotRequestID: *spotRequestID,
	}); err != nil {
		log.Printf("Failed to store runner record: %v", err)
	}

	return spotRequestID, nil
}

// CreateSpotInstanceForPipeline creates a spot instance specifically for pipeline execution
func (aws *AWSInfrastructure) CreateSpotInstanceForPipeline(ctx context.Context, runnerName, registrationToken string, labels []string) (*string, error) {
	// Generate user data script for runner installation
	userData := aws.generateUserDataScriptWithToken(runnerName, registrationToken, labels)

	// Spot instance request specification
	spotPrice := aws.config.EC2SpotPrice
	launchSpec := &ec2types.RequestSpotLaunchSpecification{
		ImageId:        aws.String(aws.config.EC2AMI),
		InstanceType:   ec2types.InstanceType(aws.config.EC2InstanceType),
		KeyName:        aws.String(aws.config.EC2KeyPairName),
		SecurityGroups: []ec2types.GroupIdentifier{{GroupId: aws.String(aws.config.EC2SecurityGroupID)}},
		SubnetId:       aws.String(aws.config.EC2SubnetID),
		UserData:       aws.String(userData),
		Monitoring: &ec2types.RunInstancesMonitoringEnabled{
			Enabled: aws.Bool(true),
		},
	}

	// Create spot instance request
	input := &ec2.RequestSpotInstancesInput{
		SpotPrice:           aws.String(spotPrice),
		InstanceCount:       aws.Int32(1),
		Type:                ec2types.SpotInstanceTypeOneTime,
		LaunchSpecification: launchSpec,
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeSpotInstancesRequest,
				Tags: []ec2types.Tag{
					{Key: aws.String("Name"), Value: aws.String(runnerName)},
					{Key: aws.String("Purpose"), Value: aws.String("github-actions-runner")},
					{Key: aws.String("RunnerName"), Value: aws.String(runnerName)},
					{Key: aws.String("ManagedBy"), Value: aws.String("github-runner-scaler-lambda")},
					{Key: aws.String("CreatedAt"), Value: aws.String(time.Now().Format(time.RFC3339))},
				},
			},
		},
	}

	result, err := aws.ec2Client.RequestSpotInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to request spot instance: %w", err)
	}

	if len(result.SpotInstanceRequests) == 0 {
		return nil, fmt.Errorf("no spot instance requests created")
	}

	spotRequestID := result.SpotInstanceRequests[0].SpotInstanceRequestId
	log.Printf("Created spot instance request: %s for runner %s", *spotRequestID, runnerName)

	// Store runner record in DynamoDB
	if err := aws.storeRunnerRecord(ctx, RunnerRecord{
		RunnerID:      runnerName,
		Status:        "pending",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		SpotRequestID: *spotRequestID,
	}); err != nil {
		log.Printf("Failed to store runner record: %v", err)
	}

	return spotRequestID, nil
}

// Generate user data script for EC2 instance with registration token
func (aws *AWSInfrastructure) generateUserDataScriptWithToken(runnerName, registrationToken string, labels []string) string {
	labelsStr := "self-hosted,linux,x64"
	if len(labels) > 0 {
		labelsStr = ""
		for i, label := range labels {
			if i > 0 {
				labelsStr += ","
			}
			labelsStr += label
		}
	}

	script := fmt.Sprintf(`#!/bin/bash
set -e

# Update system
apt-get update -y
apt-get install -y curl jq unzip awscli

# Create runner user
useradd -m -s /bin/bash runner
usermod -aG sudo runner
echo 'runner ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers

# Switch to runner user and setup runner
sudo -u runner bash << 'EOF'
cd /home/runner

# Download and install GitHub Actions runner
curl -o actions-runner-linux-x64-2.311.0.tar.gz -L https://github.com/actions/runner/releases/download/v2.311.0/actions-runner-linux-x64-2.311.0.tar.gz
tar xzf ./actions-runner-linux-x64-2.311.0.tar.gz

# Configure runner for GHE
./config.sh --url %s/orgs/%s --token %s --name %s --labels %s --work _work --replace --ephemeral

# Start runner
./run.sh &
EOF

# Signal completion
REGION=$(curl -s http://169.254.169.254/latest/meta-data/placement/region)
aws logs create-log-group --log-group-name "/aws/ec2/github-runner" --region $REGION || true
aws logs create-log-stream --log-group-name "/aws/ec2/github-runner" --log-stream-name "%s" --region $REGION || true
aws logs put-log-events --log-group-name "/aws/ec2/github-runner" --log-stream-name "%s" --log-events timestamp=$(date +%%s000),message="Runner %s started successfully" --region $REGION || true

# Keep instance alive while runner is working
while pgrep -f "Runner.Listener" > /dev/null; do
    sleep 30
done

# Self-terminate when runner job is done
aws ec2 terminate-instances --instance-ids $(curl -s http://169.254.169.254/latest/meta-data/instance-id) --region $REGION || true
`,
		aws.config.GitHubEnterpriseURL,
		aws.config.OrganizationName,
		registrationToken,
		runnerName,
		labelsStr,
		runnerName,
		runnerName,
		runnerName)

	return script
}

// TerminateRunnerInstance terminates EC2 instance by runner name
func (aws *AWSInfrastructure) TerminateRunnerInstance(ctx context.Context, runnerName string) error {
	// Find instance by tag
	input := &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:RunnerName"),
				Values: []string{runnerName},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running", "pending"},
			},
		},
	}

	result, err := aws.ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to describe instances: %w", err)
	}

	var instanceIDs []string
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			instanceIDs = append(instanceIDs, *instance.InstanceId)
		}
	}

	if len(instanceIDs) == 0 {
		log.Printf("No instances found for runner: %s", runnerName)
		return nil
	}

	// Terminate instances
	terminateInput := &ec2.TerminateInstancesInput{
		InstanceIds: instanceIDs,
	}

	_, err = aws.ec2Client.TerminateInstances(ctx, terminateInput)
	if err != nil {
		return fmt.Errorf("failed to terminate instances: %w", err)
	}

	log.Printf("Terminated %d instances for runner: %s", len(instanceIDs), runnerName)
	return nil
}

// Store runner record in DynamoDB
func (aws *AWSInfrastructure) storeRunnerRecord(ctx context.Context, record RunnerRecord) error {
	item := map[string]types.AttributeValue{
		"runner_id":        &types.AttributeValueMemberS{Value: record.RunnerID},
		"job_request_id":   &types.AttributeValueMemberN{Value: strconv.FormatInt(record.JobRequestID, 10)},
		"status":           &types.AttributeValueMemberS{Value: record.Status},
		"created_at":       &types.AttributeValueMemberS{Value: record.CreatedAt.Format(time.RFC3339)},
		"updated_at":       &types.AttributeValueMemberS{Value: record.UpdatedAt.Format(time.RFC3339)},
	}

	if record.InstanceID != "" {
		item["instance_id"] = &types.AttributeValueMemberS{Value: record.InstanceID}
	}
	if record.SpotRequestID != "" {
		item["spot_request_id"] = &types.AttributeValueMemberS{Value: record.SpotRequestID}
	}

	_, err := aws.dynamoDBClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(aws.config.DynamoDBTableName),
		Item:      item,
	})

	return err
}

// Helper functions
func (aws *AWSInfrastructure) String(s string) *string {
	return &s
}

func (aws *AWSInfrastructure) Int32(i int32) *int32 {
	return &i
}

func (aws *AWSInfrastructure) Bool(b bool) *bool {
	return &b
}

// Main Lambda handler
func Handler(ctx context.Context, event events.CloudWatchEvent) error {
	log.Printf("🚀 GitHub Runner Scaler Lambda triggered at %s", time.Now().Format(time.RFC3339))

	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize AWS infrastructure
	awsInfra, err := NewAWSInfrastructure(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to initialize AWS infrastructure: %w", err)
	}

	// Initialize GitHub Enterprise client
	gheClient := NewGHEClient(config)

	// Initialize pipeline monitor
	monitor := NewPipelineMonitor(gheClient, awsInfra, config)

	// Execute pipeline monitoring and scaling
	if err := monitor.MonitorAndScale(ctx); err != nil {
		log.Printf("❌ Pipeline monitoring failed: %v", err)
		return err
	}

	log.Printf("✅ Lambda execution completed successfully")
	return nil
}

// executeRunnerScaling contains the main logic for checking jobs and scaling runners
func executeRunnerScaling(ctx context.Context, githubClient GitHubActionsClient, awsInfra *AWSInfrastructure, config Config) error {
	log.Printf("Checking for available GitHub Actions jobs for scale set %d", config.RunnerScaleSetID)

	// Step 1: Try to get or create a message session
	session, err := awsInfra.getOrCreateSession(ctx, githubClient, config.RunnerScaleSetID)
	if err != nil {
		return fmt.Errorf("failed to get or create session: %w", err)
	}

	// Step 2: Get messages from GitHub Actions
	message, err := githubClient.GetMessage(ctx, session.MessageQueueUrl, session.MessageQueueAccessToken, 0, config.MaxRunners)
	if err != nil {
		return fmt.Errorf("failed to get message: %w", err)
	}

	// If no message, check current statistics and maintain minimum runners
	if message == nil {
		log.Printf("No new messages, maintaining current state")
		return awsInfra.maintainMinRunners(ctx, config.MinRunners)
	}

	log.Printf("Received message: ID=%d, Type=%s", message.MessageId, message.MessageType)

	// Step 3: Parse available jobs from the message
	availableJobs, err := ParseJobsFromMessage(message.Body)
	if err != nil {
		return fmt.Errorf("failed to parse jobs from message: %w", err)
	}

	log.Printf("Found %d available jobs", len(availableJobs))

	// Step 4: Calculate how many runners we need
	neededRunners := awsInfra.calculateNeededRunners(ctx, message.Statistics, len(availableJobs), config)
	log.Printf("Need %d runners based on statistics and available jobs", neededRunners)

	// Step 5: Create spot instances for needed runners
	if neededRunners > 0 {
		err := awsInfra.createRunnersForJobs(ctx, availableJobs, neededRunners)
		if err != nil {
			log.Printf("Failed to create some runners: %v", err)
		}

		// Step 6: Acquire the jobs
		if len(availableJobs) > 0 {
			jobIDs := make([]int64, len(availableJobs))
			for i, job := range availableJobs {
				jobIDs[i] = job.RunnerRequestId
			}

			acquiredJobs, err := githubClient.AcquireJobs(ctx, config.RunnerScaleSetID, session.MessageQueueAccessToken, jobIDs)
			if err != nil {
				log.Printf("Failed to acquire jobs: %v", err)
			} else {
				log.Printf("Successfully acquired %d jobs: %v", len(acquiredJobs), acquiredJobs)
			}
		}
	}

	// Step 7: Delete the processed message
	if err := githubClient.DeleteMessage(ctx, session.MessageQueueUrl, session.MessageQueueAccessToken, message.MessageId); err != nil {
		log.Printf("Failed to delete message: %v", err)
	}

	return nil
}

// getOrCreateSession retrieves an existing session from DynamoDB or creates a new one
func (aws *AWSInfrastructure) getOrCreateSession(ctx context.Context, githubClient GitHubActionsClient, scaleSetID int) (*RunnerScaleSetSession, error) {
	// Try to get existing session from DynamoDB
	session, err := aws.getSessionFromDB(ctx, scaleSetID)
	if err == nil && session != nil {
		log.Printf("Using existing session: %s", session.SessionId)
		return session, nil
	}

	// Create new session
	log.Printf("Creating new GitHub message session")
	session, err = githubClient.CreateMessageSession(ctx, scaleSetID, "lambda-runner-scaler")
	if err != nil {
		return nil, fmt.Errorf("failed to create message session: %w", err)
	}

	// Store session in DynamoDB
	if err := aws.storeSessionInDB(ctx, session); err != nil {
		log.Printf("Failed to store session in DB: %v", err)
	}

	return session, nil
}

// getSessionFromDB retrieves session from DynamoDB
func (aws *AWSInfrastructure) getSessionFromDB(ctx context.Context, scaleSetID int) (*RunnerScaleSetSession, error) {
	// Implementation for retrieving session from DynamoDB
	// For now, return nil to force creation of new session
	return nil, fmt.Errorf("session not found")
}

// storeSessionInDB stores session in DynamoDB
func (aws *AWSInfrastructure) storeSessionInDB(ctx context.Context, session *RunnerScaleSetSession) error {
	sessionRecord := SessionRecord{
		SessionID:               session.SessionId,
		MessageQueueUrl:         session.MessageQueueUrl,
		MessageQueueAccessToken: session.MessageQueueAccessToken,
		LastMessageID:           0,
		CreatedAt:               time.Now(),
		UpdatedAt:               time.Now(),
	}

	item := map[string]types.AttributeValue{
		"session_id":                  &types.AttributeValueMemberS{Value: sessionRecord.SessionID},
		"message_queue_url":           &types.AttributeValueMemberS{Value: sessionRecord.MessageQueueUrl},
		"message_queue_access_token":  &types.AttributeValueMemberS{Value: sessionRecord.MessageQueueAccessToken},
		"last_message_id":             &types.AttributeValueMemberN{Value: strconv.FormatInt(sessionRecord.LastMessageID, 10)},
		"created_at":                  &types.AttributeValueMemberS{Value: sessionRecord.CreatedAt.Format(time.RFC3339)},
		"updated_at":                  &types.AttributeValueMemberS{Value: sessionRecord.UpdatedAt.Format(time.RFC3339)},
	}

	_, err := aws.dynamoDBClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(aws.config.DynamoDBTableName + "-sessions"),
		Item:      item,
	})

	return err
}

// maintainMinRunners ensures we have at least the minimum number of runners
func (aws *AWSInfrastructure) maintainMinRunners(ctx context.Context, minRunners int) error {
	if minRunners <= 0 {
		return nil
	}

	// Get current running runners count from DynamoDB
	currentRunners, err := aws.getCurrentRunnerCount(ctx)
	if err != nil {
		log.Printf("Failed to get current runner count: %v", err)
		currentRunners = 0
	}

	needed := minRunners - currentRunners
	if needed <= 0 {
		log.Printf("Have %d runners, minimum is %d - no action needed", currentRunners, minRunners)
		return nil
	}

	log.Printf("Need to create %d additional runners to maintain minimum", needed)

	// Create the needed minimum runners
	for i := 0; i < needed; i++ {
		jobID := time.Now().UnixNano() // Use timestamp as unique job ID
		_, err := aws.CreateSpotInstance(ctx, jobID, aws.config.RunnerLabels)
		if err != nil {
			log.Printf("Failed to create minimum runner %d: %v", i+1, err)
		}
	}

	return nil
}

// getCurrentRunnerCount gets the number of currently active runners
func (aws *AWSInfrastructure) getCurrentRunnerCount(ctx context.Context) (int, error) {
	// Query DynamoDB for active runners
	// For simplicity, we'll return 0 for now
	return 0, nil
}

// calculateNeededRunners determines how many runners we need based on statistics and available jobs
func (aws *AWSInfrastructure) calculateNeededRunners(ctx context.Context, stats *RunnerScaleSetStatistic, availableJobs int, config Config) int {
	if stats == nil {
		return availableJobs
	}

	// Calculate based on:
	// 1. Available jobs that need runners
	// 2. Current assigned jobs without runners
	// 3. Minimum runners requirement
	// 4. Maximum runners limit

	needed := availableJobs + stats.TotalAssignedJobs - stats.TotalRegisteredRunners

	// Ensure we don't go below minimum
	if needed < config.MinRunners {
		needed = config.MinRunners
	}

	// Ensure we don't exceed maximum
	if needed > config.MaxRunners {
		needed = config.MaxRunners
	}

	// Don't create negative runners
	if needed < 0 {
		needed = 0
	}

	return needed
}

// createRunnersForJobs creates spot instances for the given jobs
func (aws *AWSInfrastructure) createRunnersForJobs(ctx context.Context, jobs []*JobAvailable, maxRunners int) error {
	created := 0
	for i, job := range jobs {
		if created >= maxRunners {
			break
		}

		labels := job.RequestLabels
		if len(labels) == 0 {
			labels = aws.config.RunnerLabels
		}

		_, err := aws.CreateSpotInstance(ctx, job.RunnerRequestId, labels)
		if err != nil {
			log.Printf("Failed to create runner for job %d: %v", job.RunnerRequestId, err)
			continue
		}

		created++
		log.Printf("Created runner %d/%d for job %d", i+1, maxRunners, job.RunnerRequestId)
	}

	return nil
}

// Schedule next execution using EventBridge
func (aws *AWSInfrastructure) ScheduleNextExecution(ctx context.Context) error {
	// Create EventBridge rule for next execution (60 seconds from now)
	ruleName := "github-runner-scaler-schedule"
	scheduleExpression := "rate(1 minute)"

	putRuleInput := &eventbridge.PutRuleInput{
		Name:               aws.String(ruleName),
		ScheduleExpression: aws.String(scheduleExpression),
		State:              "ENABLED",
		Description:        aws.String("Schedule GitHub Runner Scaler Lambda execution every 60 seconds"),
	}

	_, err := aws.eventBridgeClient.PutRule(ctx, putRuleInput)
	if err != nil {
		return fmt.Errorf("failed to create EventBridge rule: %w", err)
	}

	log.Printf("Scheduled next execution in 60 seconds")
	return nil
}

func main() {
	lambda.Start(Handler)
} 